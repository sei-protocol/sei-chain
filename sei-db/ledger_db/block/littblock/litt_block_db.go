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

const (
	blocksTableName = "blocks"
	qcsTableName    = "qcs"
)

var _ types.BlockDB = (*blockDB)(nil)

// blockDB is a durable types.BlockDB backed by LittDB
type blockDB struct {
	db     littdb.DB
	blocks littdb.Table
	qcs    littdb.Table

	// watermark is the highest n passed to PruneBefore; the GC filters treat
	// keys strictly below it as eligible for reclamation. Read from the GC
	// goroutine, so accessed atomically.
	watermark atomic.Uint64

	// Write-order cursors (see types.BlockDB contract). Guarded by mu.
	mu              sync.Mutex
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber
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

	// Both tables run with a single write shard. The block store relies on
	// LittDB's single-shard in-write-order crash atomicity (after a crash the
	// surviving writes form a contiguous prefix of the write order, never a
	// gapped subset), which the write-order cursors and contiguous-QC recovery
	// depend on. ShardingFactor > 1 would void that guarantee.
	blocksConfig := littdb.DefaultTableConfig(blocksTableName)
	blocksConfig.TTL = config.Retention
	blocksConfig.GCFilter = s.blocksGCFilter
	blocksConfig.ShardingFactor = 1
	blocksTable, err := db.BuildTable(blocksConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build blocks table: %w", err)
	}

	qcsConfig := littdb.DefaultTableConfig(qcsTableName)
	qcsConfig.TTL = config.Retention
	qcsConfig.GCFilter = s.qcsGCFilter
	qcsConfig.ShardingFactor = 1
	qcsTable, err := db.BuildTable(qcsConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build qcs table: %w", err)
	}

	s.blocks = blocksTable
	s.qcs = qcsTable

	if err := s.recoverCursors(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to recover write cursors: %w", err)
	}
	return s, nil
}

// recoverCursors reloads the write-order cursors (lastBlockNumber, lastQCNext,
// and their presence flags) from on-disk state. Without this, a reopened DB
// would treat itself as empty and let WriteBlock/WriteQC silently accept
// out-of-order or non-contiguous writes that overwrite or gap persisted data.
//
// Blocks are written in strictly ascending number order, so the newest primary
// key is the highest block number (block hash aliases are secondary keys and
// are skipped by GetNewestKey). QCs are written contiguously, so the newest
// primary key is the last QC's lowerBound; its upperBound — which is what
// lastQCNext tracks — is lowerBound + len(headers), since a QC's header count
// equals the size of the half-open range it finalizes (see types.WriteQC).
func (s *blockDB) recoverCursors() error {
	blockKey, exists, err := s.blocks.GetNewestKey()
	if err != nil {
		return fmt.Errorf("failed to read newest block key: %w", err)
	}
	if exists {
		s.lastBlockNumber = decodeKey(blockKey)
		s.hasBlocks = true
	}

	qcKey, exists, err := s.qcs.GetNewestKey()
	if err != nil {
		return fmt.Errorf("failed to read newest qc key: %w", err)
	}
	if exists {
		lowerBound := decodeKey(qcKey)
		value, ok, err := s.qcs.Get(qcKey)
		if err != nil {
			return fmt.Errorf("failed to read newest qc value: %w", err)
		}
		if !ok {
			return fmt.Errorf("newest qc key %d has no value", lowerBound)
		}
		qc, err := decodeQC(value)
		if err != nil {
			return fmt.Errorf("failed to unmarshal newest qc: %w", err)
		}
		s.lastQCNext = lowerBound + types.GlobalBlockNumber(len(qc.Headers()))
		s.hasQC = true
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

	value := encodeBlock(blk)
	hash := blk.Header().Hash()
	hashAlias := &litttypes.SecondaryKey{
		Key:    hash.Bytes(),
		Offset: 0,
		Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
	}
	if err := s.blocks.Put(encodeKey(n), value, hashAlias); err != nil {
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
			Key:    encodeKey(m),
			Offset: 0,
			Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
		})
	}
	if err := s.qcs.Put(encodeKey(lowerBound), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC [%d,%d): %w", lowerBound, upperBound, err)
	}

	s.lastQCNext = upperBound
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(n types.GlobalBlockNumber) error {
	for {
		cur := s.watermark.Load()
		if uint64(n) <= cur {
			return nil
		}
		if s.watermark.CompareAndSwap(cur, uint64(n)) {
			return nil
		}
	}
}

// blocksGCFilter marks a blocks-table key as reclaimable once its block number
// is strictly below the prune watermark. Non-primary keys are the header-hash
// aliases, which share their block's segment, so they always pass — the block's
// primary number key is what actually gates segment reclamation.
func (s *blockDB) blocksGCFilter(key []byte, isPrimaryKey bool) (bool, error) {
	if !isPrimaryKey {
		return true, nil
	}
	return uint64(decodeKey(key)) < s.watermark.Load(), nil
}

// qcsGCFilter marks a qcs-table key as reclaimable once its number is strictly
// below the watermark. Every key (primary First and the per-covered-number
// secondaries) is a number, so a QC's segment is reclaimable only once its
// highest covered number (Next-1) is below the watermark — i.e. once
// Next <= watermark. A QC straddling the watermark is retained.
func (s *blockDB) qcsGCFilter(key []byte, _ bool) (bool, error) {
	return uint64(decodeKey(key)) < s.watermark.Load(), nil
}

func (s *blockDB) Flush() error {
	// Flush the two tables concurrently: each Flush is an independent fsync, so
	// running one in a goroutine roughly halves the wall-clock flush latency.
	qcErrCh := make(chan error, 1)
	go func() {
		qcErrCh <- s.qcs.Flush()
	}()

	blocksErr := s.blocks.Flush()
	qcsErr := <-qcErrCh

	if blocksErr != nil {
		return fmt.Errorf("failed to flush blocks table: %w", blocksErr)
	}
	if qcsErr != nil {
		return fmt.Errorf("failed to flush qcs table: %w", qcsErr)
	}
	return nil
}

func (s *blockDB) Blocks(reverse bool) (types.BlockIterator, error) {
	it, err := s.blocks.Iterator(reverse)
	if err != nil {
		return nil, fmt.Errorf("failed to open blocks iterator: %w", err)
	}
	return &blockIterator{it: it}, nil
}

func (s *blockDB) QCs(reverse bool) (types.QCIterator, error) {
	it, err := s.qcs.Iterator(reverse)
	if err != nil {
		return nil, fmt.Errorf("failed to open qcs iterator: %w", err)
	}
	return &qcIterator{it: it}, nil
}

func (s *blockDB) ReadBlockByNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.Block], error) {
	return getBlock(s.blocks, encodeKey(n))
}

func (s *blockDB) ReadBlockByHash(
	hash types.BlockHeaderHash,
) (utils.Option[*types.Block], error) {
	return getBlock(s.blocks, hash.Bytes())
}

func getBlock(table littdb.Table, key []byte) (utils.Option[*types.Block], error) {
	value, exists, err := table.Get(key)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[*types.Block](), nil
	}
	blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(blk), nil
}

func (s *blockDB) ReadQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	value, exists, err := s.qcs.Get(encodeKey(n))
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

// ForceGC runs a synchronous garbage-collection pass over the tables backing db,
// so any pending prune takes effect immediately rather than on the periodic GC
// schedule. db must be a *blockDB returned by NewBlockDB. Intended for tests and
// operational tooling.
func ForceGC(db types.BlockDB) error {
	impl, ok := db.(*blockDB)
	if !ok {
		return fmt.Errorf("ForceGC: db is not a littblock block store (%T)", db)
	}
	for _, t := range []littdb.Table{impl.blocks, impl.qcs} {
		managed, ok := t.(littdb.ManagedTable)
		if !ok {
			return fmt.Errorf("table %q is not a ManagedTable", t.Name())
		}
		if err := managed.RunGC(); err != nil {
			return fmt.Errorf("failed to run GC on table %q: %w", t.Name(), err)
		}
	}
	return nil
}
