package littblock

import (
	"fmt"
	"sync"
	"sync/atomic"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ledgerTableName is the single table holding both blocks and QCs. They share
// one table so a crash leaves a contiguous write-order prefix spanning both
// record kinds (see NewBlockDB), which is what guarantees a persisted block is
// always covered by a persisted QC.
const ledgerTableName = "ledger"

var _ types.BlockDB = (*blockDB)(nil)

// blockDB is a durable types.BlockDB backed by LittDB
type blockDB struct {
	db    littdb.DB
	table littdb.Table

	// watermark is a retention floor, always a QC boundary (a GlobalRange().First):
	// PruneBefore rounds a requested prune point down to the start of the cohort
	// containing it, and startup re-derives it as the lowest surviving QC's First
	// (see cohortStart and recoverReadWatermark). Keeping it on a cohort boundary
	// is what makes a QC's blocks change readability atomically — the gate never
	// splits a cohort.
	//
	// It serves two purposes: the GC filters treat keys strictly below it as
	// eligible for reclamation, and reads and iterators refuse to serve any block
	// strictly below it. The read gate is what upholds the "a served block is
	// always covered by a served QC" invariant: asynchronous GC can strand a
	// below-watermark block on disk (its covering QC reclaimed while the block's
	// own segment lingers), but such a block is never served. Read from the GC
	// goroutine, so accessed atomically.
	watermark atomic.Uint64

	// Write-order cursors (see types.BlockDB contract). Guarded by mu.
	mu              sync.Mutex
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber

	// latestQCStartBlock is the most recently written QC's starting block number.
	latestQCStartBlock types.GlobalBlockNumber

	// oldestQCStart is where the oldest QC this handle has seen begins. Iterator clamps its
	// start up to it, which is what lets the positioned lookup always land on a retained QC
	// record: a start below every QC's range has no key to position at. Set when the first QC
	// is written and re-derived on open (see recoverReadWatermark).
	//
	// It is a floor, not an exact value — GC may later reclaim that QC — but by then
	// PruneBefore has advanced watermark past it, and Iterator clamps to both. Meaningful
	// only while hasQC.
	oldestQCStart types.GlobalBlockNumber
}

// NewBlockDB opens (or creates) a LittDB-backed types.BlockDB from config. The
// underlying LittDB is built from config.Litt, and the two tables apply
// config.Retention as a TTL failsafe (pruning never reclaims data younger than
// that even once the watermark has advanced past it).
func NewBlockDB(config *LittBlockConfig) (types.BlockDB, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid block db config: %w", err)
	}
	db, err := littbuilder.NewDB(config.Litt)
	if err != nil {
		return nil, fmt.Errorf("failed to open litt db: %w", err)
	}

	s := &blockDB{db: db}

	// Blocks and QCs live in one table with a single write shard. The block
	// store relies on LittDB's single-shard in-write-order crash atomicity
	// (after a crash the surviving writes form a contiguous prefix of the write
	// order, never a gapped subset). Because the covering QC is always written
	// before the block (WriteBlock rejects an uncovered block), that prefix
	// guarantees a persisted block is always covered by a persisted QC. It also
	// backs the write-order cursors and contiguous-QC recovery. ShardingFactor
	// > 1, or splitting blocks and QCs across two tables, would void this.
	tableConfig := littdb.DefaultTableConfig(ledgerTableName)
	tableConfig.TTL = config.Retention
	tableConfig.GCFilter = s.gcFilter
	tableConfig.ShardingFactor = 1 // DO NOT CHANGE!!
	table, err := db.BuildTable(tableConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build ledger table: %w", err)
	}

	s.table = table

	if err := s.recoverCursors(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to recover write cursors: %w", err)
	}
	if err := s.recoverReadWatermark(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to recover read watermark: %w", err)
	}
	return s, nil
}

// recoverCursors reloads the write-order cursors (lastBlockNumber, lastQCNext,
// and their presence flags) from on-disk state. Without this, a reopened DB
// would treat itself as empty and let WriteBlock/WriteQC silently accept
// out-of-order or non-contiguous writes that overwrite or gap persisted data.
func (s *blockDB) recoverCursors() error {
	it, err := s.table.Iterator(true)
	if err != nil {
		return fmt.Errorf("failed to open recovery iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	for !s.hasBlocks || !s.hasQC {
		ok, err := it.Next()
		if err != nil {
			return fmt.Errorf("failed to advance recovery iterator: %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return fmt.Errorf("failed to read recovery key: %w", err)
		}
		if !isPrimary {
			continue
		}
		switch keyKind(key) {
		case kindBlock:
			if !s.hasBlocks {
				s.lastBlockNumber = decodeNumberKey(key)
				s.hasBlocks = true
			}
		case kindQC:
			if !s.hasQC {
				value, err := it.GetValue()
				if err != nil {
					return fmt.Errorf("failed to read newest qc value: %w", err)
				}
				qc, err := decodeQC(value)
				if err != nil {
					return fmt.Errorf("failed to unmarshal newest qc: %w", err)
				}
				s.latestQCStartBlock, s.lastQCNext = coveredRange(qc)
				s.hasQC = true
			}
		}
	}
	return nil
}

// recoverReadWatermark re-derives a safe read watermark on open. The watermark
// is in-memory only, so a restart forgets every PruneBefore. That is fine for
// reclamation (nothing new is deleted), but we must protect against showing un-pruned
// blocks with pruned QCs.
func (s *blockDB) recoverReadWatermark() error {
	it, err := s.table.Iterator(false)
	if err != nil {
		return fmt.Errorf("failed to open watermark recovery iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	for {
		ok, err := it.Next()
		if err != nil {
			return fmt.Errorf("failed to advance watermark recovery iterator: %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return fmt.Errorf("failed to read watermark recovery key: %w", err)
		}
		if !isPrimary || keyKind(key) != kindQC {
			continue
		}
		oldest := decodeNumberKey(key)
		s.watermark.Store(uint64(oldest))
		s.oldestQCStart = oldest
		return nil
	}

	if s.hasBlocks {
		// No QC survives. The never-empty prune invariant guarantees at least one
		// (block, QC) pair is always retained, so blocks-without-QC is unreachable
		// through normal operation — it means the store is corrupt (e.g. a QC WAL
		// file was removed out of band). Refuse to open rather than serve blocks we
		// can no longer trust.
		return fmt.Errorf("corrupt store: newest block %d has no surviving QC covering it", s.lastBlockNumber)
	}
	return nil
}

func (s *blockDB) WriteBlock(n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasBlocks && n != s.lastBlockNumber+1 {
		return fmt.Errorf("block number %d not contiguous with last written %d: %w",
			n, s.lastBlockNumber, types.ErrBlockOutOfOrder)
	}
	// A covering QC must already be written. Since QCs are contiguous and blocks
	// strictly ascending, n is covered iff n < lastQCNext. This guard also fixes
	// the QC-before-block write order: the covering QC's Put has already issued
	// under this mutex, so on a crash a surviving block implies a surviving QC.
	if !s.hasQC || n >= s.lastQCNext {
		return fmt.Errorf("block number %d not covered by any written QC (next QC bound %d): %w",
			n, s.lastQCNext, types.ErrBlockMissingQC)
	}

	value := encodeBlock(n, blk)
	hash := blk.Header().Hash()
	hashAlias := &litttypes.SecondaryKey{
		Key:    blockHashKey(hash),
		Offset: 0,
		Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
	}
	if err := s.table.Put(blockKey(n), value, hashAlias); err != nil {
		return fmt.Errorf("failed to put block %d: %w", n, err)
	}

	s.lastBlockNumber = n
	s.hasBlocks = true
	return nil
}

func (s *blockDB) WriteQC(qc *types.FullCommitQC) error {
	first, next := coveredRange(qc)
	if first >= next {
		return fmt.Errorf("QC at %d covers no blocks: %w", first, types.ErrQCNonContiguous)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasQC && first != s.lastQCNext {
		return fmt.Errorf("QC starts at %d, expected %d: %w",
			first, s.lastQCNext, types.ErrQCNonContiguous)
	}

	value := encodeQC(qc)
	var aliases []*litttypes.SecondaryKey
	for m := first + 1; m < next; m++ {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key:    qcKey(m),
			Offset: 0,
			Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
		})
	}
	if err := s.table.Put(qcKey(first), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC [%d,%d): %w", first, next, err)
	}

	if !s.hasQC {
		// The first QC may start anywhere its caller allows, and nothing below it will ever
		// be written. Record where coverage begins so Iterator can clamp to it without
		// discovering it by scanning; a reopen re-derives the same value.
		s.oldestQCStart = first
	}
	s.latestQCStartBlock = first
	s.lastQCNext = next
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(blockHeight types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasBlocks {
		// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
		// and is technically a legal implementation of the contract in the godocs.
		return nil
	}

	if ceiling := min(s.latestQCStartBlock, s.lastBlockNumber); blockHeight > ceiling {
		blockHeight = ceiling
	}

	// Round the watermark down to the start of a QC's range, to avoid pruning a QC before its blocks.
	blockHeight, err := s.clampPruneBoundary(blockHeight)
	if err != nil {
		return err
	}

	// Advance the watermark monotonically. mu serializes writers and PruneBefore
	// is the only one, so a plain load/store suffices; the field stays atomic
	// only because the GC filter and readers load it without holding mu.
	if uint64(blockHeight) > s.watermark.Load() {
		s.watermark.Store(uint64(blockHeight))
	}
	return nil
}

// clampPruneBoundary returns the start of the QC that covers n, or n if there is no QC covering N
// (which can happen if you prune the same n twice).
func (s *blockDB) clampPruneBoundary(blockHeight types.GlobalBlockNumber) (types.GlobalBlockNumber, error) {
	value, exists, err := s.table.Get(qcKey(blockHeight))
	if err != nil {
		return 0, fmt.Errorf("failed to read covering QC for %d: %w", blockHeight, err)
	}
	if !exists {
		return blockHeight, nil
	}
	qc, err := decodeQC(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode covering QC for %d: %w", blockHeight, err)
	}
	return qc.QC().GlobalRange().First, nil
}

// gcFilter marks a key in the shared ledger table as reclaimable, dispatching on
// its kind prefix:
//
//   - block-number keys are reclaimable once the block number is strictly below
//     the prune watermark;
//   - QC keys (the primary First and every per-covered-number secondary) are
//     reclaimable once their number is below the watermark, so a QC's segment is
//     reclaimable only once its highest covered number (Next-1) is below the
//     watermark — i.e. once Next <= watermark; a QC straddling the watermark is
//     retained;
//   - header-hash aliases share their block's segment, so they always pass — the
//     block's primary number key is what actually gates segment reclamation.
func (s *blockDB) gcFilter(key []byte, _ bool) (bool, error) {
	switch keyKind(key) {
	case kindBlock, kindQC:
		return uint64(decodeNumberKey(key)) < s.watermark.Load(), nil
	case kindBlockHash:
		return true, nil
	default:
		return false, fmt.Errorf("unknown ledger key kind %q", key[0])
	}
}

func (s *blockDB) Flush() error {
	if err := s.table.Flush(); err != nil {
		return fmt.Errorf("failed to flush ledger table: %w", err)
	}
	return nil
}

func (s *blockDB) Status() types.DBStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tips types.DBStatus
	if s.hasBlocks {
		tips.NextBlock = s.lastBlockNumber + 1
	}
	if s.hasQC {
		tips.NextQC = s.lastQCNext
	}
	return tips
}

func (s *blockDB) Iterator(n types.GlobalBlockNumber) (types.BlockDBIterator, error) {
	watermark := s.watermark.Load()
	nextQC, oldestQCStart := s.qcCoverage()

	// Clamp up to the lowest number a retained QC covers. Both floors are needed: watermark is
	// the retention gate, oldestQCStart is where coverage begins on a store that never had data
	// below it (bootstrapped mid-chain), and either may be the higher of the two.
	start := n
	if uint64(start) < watermark {
		start = types.GlobalBlockNumber(watermark)
	}
	if start < oldestQCStart {
		start = oldestQCStart
	}

	if start >= nextQC {
		// Nothing is covered at or above start. NextQC is zero on an empty store, which this
		// comparison also classifies as past-coverage.
		return &blockDBIterator{}, nil
	}

	// A QC is stored under its First as the primary key with a covered-number alias for every
	// other number in its range, and an alias carries the full QC value. Positioning the scan at
	// qcKey(start) therefore lands on the covering QC no matter where start falls in its range.
	it, found, err := s.table.IteratorAt(qcKey(start), false)
	if err != nil {
		return nil, fmt.Errorf("failed to open iterator at %d: %w", start, err)
	}
	if !found {
		// start has been clamped into [oldestQCStart, NextQC), and retained QCs cover that
		// interval contiguously, so some QC's primary or covered-number alias is stored under
		// qcKey(start). A miss means a record that must exist does not — a truncated key file,
		// or a segment missing from the readable snapshot. Refuse rather than fall back to a
		// full scan: the caller asked to start at a height precisely to avoid reading what lies
		// below it, and a scan would silently do the opposite.
		return nil, fmt.Errorf("corrupt store: no QC record at %d despite coverage to %d", start, nextQC)
	}
	return &blockDBIterator{
		it:            it,
		watermark:     watermark,
		startN:        start,
		expectStartQC: true,
	}, nil
}

// qcCoverage returns the exclusive upper bound of QC coverage and the lowest number a retained
// QC covers. Both are zero when no QC has been written.
func (s *blockDB) qcCoverage() (nextQC types.GlobalBlockNumber, oldestQCStart types.GlobalBlockNumber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasQC {
		return 0, 0
	}
	return s.lastQCNext, s.oldestQCStart
}

func (s *blockDB) ReadBlockByNumber(n types.GlobalBlockNumber) (utils.Option[*types.Block], error) {
	// Refuse below-watermark blocks: they may be stranded (covering QC reclaimed).
	if uint64(n) < s.watermark.Load() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	result, err := getBlock(s.table, blockKey(n))
	if err != nil {
		return utils.None[*types.Block](), err
	}
	if bwn, ok := result.Get(); ok {
		return utils.Some(bwn.Block), nil
	}
	return utils.None[*types.Block](), nil
}

func (s *blockDB) ReadBlockByHash(hash types.BlockHeaderHash) (utils.Option[types.BlockWithNumber], error) {
	result, err := getBlock(s.table, blockHashKey(hash))
	if err != nil {
		return utils.None[types.BlockWithNumber](), err
	}
	// The number is not known until the block is resolved; refuse it if it turns
	// out to be below the watermark (potentially stranded from its covering QC).
	if bwn, ok := result.Get(); ok && uint64(bwn.Number) < s.watermark.Load() {
		return utils.None[types.BlockWithNumber](), nil
	}
	return result, nil
}

func getBlock(table littdb.Table, key []byte) (utils.Option[types.BlockWithNumber], error) {
	value, exists, err := table.Get(key)
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[types.BlockWithNumber](), nil
	}
	n, blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(types.BlockWithNumber{Block: blk, Number: n}), nil
}

func (s *blockDB) ReadQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	// Below-watermark blocks are not served, so neither is their covering QC.
	if uint64(n) < s.watermark.Load() {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	value, exists, err := s.table.Get(qcKey(n))
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to read QC: %w", err)
	}
	if !exists {
		return utils.None[*types.FullCommitQC](), nil
	}
	qc, err := decodeQC(value)
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	return utils.Some(qc), nil
}

func (s *blockDB) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close litt db: %w", err)
	}
	return nil
}

// ForceGC runs a synchronous garbage-collection pass over the table backing db,
// so any pending prune takes effect immediately rather than on the periodic GC
// schedule. db must be a *blockDB returned by NewBlockDB. Intended for tests and
// operational tooling.
func ForceGC(db types.BlockDB) error {
	impl, ok := db.(*blockDB)
	if !ok {
		return fmt.Errorf("ForceGC: db is not a littblock block store (%T)", db)
	}
	managed, ok := impl.table.(littdb.ManagedTable)
	if !ok {
		return fmt.Errorf("table %q is not a ManagedTable", impl.table.Name())
	}
	if err := managed.RunGC(); err != nil {
		return fmt.Errorf("failed to run GC on table %q: %w", impl.table.Name(), err)
	}
	return nil
}
