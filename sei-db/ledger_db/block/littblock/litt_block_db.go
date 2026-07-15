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

	// watermark is the highest n passed to PruneBefore (a floor re-derived at
	// startup, see recoverReadWatermark). It serves two purposes: the GC filters
	// treat keys strictly below it as eligible for reclamation, and reads and
	// iterators refuse to serve any block strictly below it. The read gate is what
	// upholds the "a served block is always covered by a served QC" invariant:
	// asynchronous GC can strand a below-watermark block on disk (its covering QC
	// reclaimed while the block's own segment lingers), but such a block is never
	// served. Read from the GC goroutine, so accessed atomically.
	watermark atomic.Uint64

	// Write-order cursors (see types.BlockDB contract). Guarded by mu.
	mu              sync.Mutex
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber

	// latestQCStartBlock is the most recently written QC's starting block number.
	latestQCStartBlock types.GlobalBlockNumber
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
		key, isPrimary := it.GetKey()
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
				lowerBound := decodeNumberKey(key)
				value, err := it.GetValue()
				if err != nil {
					return fmt.Errorf("failed to read newest qc value: %w", err)
				}
				qc, err := decodeQC(value)
				if err != nil {
					return fmt.Errorf("failed to unmarshal newest qc: %w", err)
				}
				s.latestQCStartBlock = lowerBound
				s.lastQCNext = lowerBound + types.GlobalBlockNumber(len(qc.Headers()))
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
		key, isPrimary := it.GetKey()
		if !isPrimary || keyKind(key) != kindQC {
			continue
		}
		s.watermark.Store(uint64(decodeNumberKey(key)))
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
	if s.hasBlocks && n <= s.lastBlockNumber {
		return fmt.Errorf("block number %d not greater than last written %d: %w",
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

func (s *blockDB) WriteQC(
	lowerBound types.GlobalBlockNumber,
	upperBound types.GlobalBlockNumber,
	qc *types.FullCommitQC,
) error {
	if lowerBound >= upperBound {
		return fmt.Errorf("QC lowerBound %d >= upperBound %d: %w",
			lowerBound, upperBound, types.ErrQCNonContiguous)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasQC && lowerBound != s.lastQCNext {
		return fmt.Errorf("QC lowerBound %d != expected %d: %w",
			lowerBound, s.lastQCNext, types.ErrQCNonContiguous)
	}

	value := encodeQC(qc)
	var aliases []*litttypes.SecondaryKey
	for m := lowerBound + 1; m < upperBound; m++ {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key:    qcKey(m),
			Offset: 0,
			Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
		})
	}
	if err := s.table.Put(qcKey(lowerBound), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC [%d,%d): %w", lowerBound, upperBound, err)
	}

	s.latestQCStartBlock = lowerBound
	s.lastQCNext = upperBound
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(n types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasBlocks {
		// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
		// and is technically a legal implementation of the contract in the godocs.
		return nil
	}

	if ceiling := min(s.latestQCStartBlock, s.lastBlockNumber); n > ceiling {
		n = ceiling
	}

	// Advance the watermark monotonically. mu serializes writers and PruneBefore
	// is the only one, so a plain load/store suffices; the field stays atomic
	// only because the GC filter and readers load it without holding mu.
	if uint64(n) > s.watermark.Load() {
		s.watermark.Store(uint64(n))
	}
	return nil
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

func (s *blockDB) Blocks(reverse bool) (types.BlockIterator, error) {
	it, err := s.table.Iterator(reverse)
	if err != nil {
		return nil, fmt.Errorf("failed to open blocks iterator: %w", err)
	}
	return &blockIterator{it: it, watermark: s.watermark.Load()}, nil
}

func (s *blockDB) QCs(reverse bool) (types.QCIterator, error) {
	it, err := s.table.Iterator(reverse)
	if err != nil {
		return nil, fmt.Errorf("failed to open qcs iterator: %w", err)
	}
	return &qcIterator{it: it, watermark: s.watermark.Load()}, nil
}

func (s *blockDB) ReadBlockByNumber(n types.GlobalBlockNumber) (utils.Option[*types.Block], error) {
	// Refuse below-watermark blocks: they may be stranded (covering QC reclaimed).
	if uint64(n) < s.watermark.Load() {
		return utils.None[*types.Block](), nil
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
		return utils.None[*types.FullCommitQC](), nil
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
