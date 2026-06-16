package litt

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	blocksTableName = "blocks"
	qcsTableName    = "qcs"

	// defaultRetention is the failsafe minimum age before any record may be
	// reclaimed, regardless of the prune watermark. See PruneBefore.
	defaultRetention = 24 * time.Hour
)

var _ block.BlockDB = (*blockDB)(nil)

// blockDB is a durable block.BlockDB backed by LittDB. It uses two tables:
//
//   - "blocks": primary key = GlobalBlockNumber (big-endian u64), value =
//     marshaled block, secondary key = block header hash (for ReadBlockByHash).
//   - "qcs": primary key = GlobalRange().First, value = marshaled FullCommitQC,
//     secondary keys = every covered number in (First, Next) so a QC is
//     retrievable by any block number it finalizes.
//
// Both tables iterate in insertion order; callers write blocks/QCs in ascending
// order (enforced below), so forward iteration yields ascending order.
type blockDB struct {
	db        littdb.DB
	blocks    littdb.Table
	qcs       littdb.Table
	committee *types.Committee

	// watermark is the highest n passed to PruneBefore; the GC filters treat
	// keys strictly below it as eligible for reclamation. Read from the GC
	// goroutine, so accessed atomically.
	watermark atomic.Uint64

	// Write-order cursors (see block.BlockDB contract). Guarded by mu.
	mu              sync.Mutex
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber
}

// NewBlockDB opens (or creates) a LittDB-backed block.BlockDB rooted at dir.
// committee is used to compute each QC's GlobalRange. retention is a failsafe
// minimum age before any record may be reclaimed (a non-positive value falls
// back to 24h); pruning never reclaims data younger than this even once the
// watermark has advanced past it.
func NewBlockDB(dir string, committee *types.Committee, retention time.Duration) (block.BlockDB, error) {
	if retention <= 0 {
		retention = defaultRetention
	}

	config, err := littdb.DefaultConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to build litt config: %w", err)
	}
	db, err := littbuilder.NewDB(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open litt db: %w", err)
	}

	s := &blockDB{db: db, committee: committee}

	blocksConfig := littdb.DefaultTableConfig(blocksTableName)
	blocksConfig.TTL = retention
	blocksConfig.GCFilter = s.blocksGCFilter
	blocksTable, err := db.BuildTable(blocksConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build blocks table: %w", err)
	}

	qcsConfig := littdb.DefaultTableConfig(qcsTableName)
	qcsConfig.TTL = retention
	qcsConfig.GCFilter = s.qcsGCFilter
	qcsTable, err := db.BuildTable(qcsConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build qcs table: %w", err)
	}

	s.blocks = blocksTable
	s.qcs = qcsTable
	return s, nil
}

func encodeKey(n types.GlobalBlockNumber) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

func decodeKey(b []byte) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(binary.BigEndian.Uint64(b))
}

func (s *blockDB) WriteBlock(_ context.Context, n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasBlocks && n <= s.lastBlockNumber {
		return fmt.Errorf("block number %d not greater than last written %d: %w",
			n, s.lastBlockNumber, block.ErrBlockOutOfOrder)
	}

	value := types.BlockConv.Marshal(blk)
	hash := blk.Header().Hash()
	hashAlias := &litttypes.SecondaryKey{Key: hash.Bytes(), Offset: 0, Length: uint32(len(value))} //nolint:gosec // value length fits u32 (litt value cap is 2^32)
	if err := s.blocks.Put(encodeKey(n), value, hashAlias); err != nil {
		return fmt.Errorf("failed to put block %d: %w", n, err)
	}

	s.lastBlockNumber = n
	s.hasBlocks = true
	return nil
}

func (s *blockDB) WriteQC(_ context.Context, qc *types.FullCommitQC) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := qc.QC().GlobalRange(s.committee)
	if s.hasQC && r.First != s.lastQCNext {
		return fmt.Errorf("QC GlobalRange().First %d != expected %d: %w",
			r.First, s.lastQCNext, block.ErrQCNonContiguous)
	}

	value := types.FullCommitQCConv.Marshal(qc)
	var aliases []*litttypes.SecondaryKey
	for m := r.First + 1; m < r.Next; m++ {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key:    encodeKey(m),
			Offset: 0,
			Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
		})
	}
	if err := s.qcs.Put(encodeKey(r.First), value, aliases...); err != nil {
		return fmt.Errorf("failed to put QC [%d,%d): %w", r.First, r.Next, err)
	}

	s.lastQCNext = r.Next
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(_ context.Context, n types.GlobalBlockNumber) error {
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

func (s *blockDB) Flush(_ context.Context) error {
	if err := s.blocks.Flush(); err != nil {
		return fmt.Errorf("failed to flush blocks table: %w", err)
	}
	if err := s.qcs.Flush(); err != nil {
		return fmt.Errorf("failed to flush qcs table: %w", err)
	}
	return nil
}

func (s *blockDB) Blocks(_ context.Context) (block.BlockIterator, error) {
	it, err := s.blocks.Iterator(false)
	if err != nil {
		return nil, fmt.Errorf("failed to open blocks iterator: %w", err)
	}
	return &blockIterator{it: it}, nil
}

func (s *blockDB) QCs(_ context.Context) (block.QCIterator, error) {
	it, err := s.qcs.Iterator(false)
	if err != nil {
		return nil, fmt.Errorf("failed to open qcs iterator: %w", err)
	}
	return &qcIterator{it: it}, nil
}

func (s *blockDB) ReadBlockByNumber(
	_ context.Context,
	n types.GlobalBlockNumber,
) (utils.Option[*types.Block], error) {
	return getBlock(s.blocks, encodeKey(n))
}

func (s *blockDB) ReadBlockByHash(
	_ context.Context,
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
	blk, err := types.BlockConv.Unmarshal(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(blk), nil
}

func (s *blockDB) ReadQCByBlockNumber(
	_ context.Context,
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	value, exists, err := s.qcs.Get(encodeKey(n))
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to read QC: %w", err)
	}
	if !exists {
		return utils.None[*types.FullCommitQC](), nil
	}
	qc, err := types.FullCommitQCConv.Unmarshal(value)
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	return utils.Some(qc), nil
}

func (s *blockDB) Close(_ context.Context) error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close litt db: %w", err)
	}
	return nil
}

// forceGC runs garbage collection on both tables. Test-only helper used by the
// conformance suite to make pruning observable without waiting for the periodic
// GC.
func (s *blockDB) forceGC() error {
	for _, t := range []littdb.Table{s.blocks, s.qcs} {
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

var (
	_ block.BlockIterator = (*blockIterator)(nil)
	_ block.QCIterator    = (*qcIterator)(nil)
)

// blockIterator wraps a litt iterator, skipping secondary (hash-alias) keys so
// it yields one entry per block.
type blockIterator struct {
	it littdb.Iterator
}

func (b *blockIterator) Next() (bool, error) {
	for {
		ok, err := b.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance blocks iterator: %w", err)
		}
		if !ok {
			return false, nil
		}
		if _, isPrimary := b.it.GetKey(); isPrimary {
			return true, nil
		}
	}
}

func (b *blockIterator) Number() types.GlobalBlockNumber {
	key, _ := b.it.GetKey()
	return decodeKey(key)
}

func (b *blockIterator) Block() (*types.Block, error) {
	value, err := b.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read block value: %w", err)
	}
	blk, err := types.BlockConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return blk, nil
}

func (b *blockIterator) Close() error {
	if err := b.it.Close(); err != nil {
		return fmt.Errorf("failed to close blocks iterator: %w", err)
	}
	return nil
}

// qcIterator wraps a litt iterator, skipping secondary (covered-number) keys so
// it yields one entry per QC.
type qcIterator struct {
	it littdb.Iterator
}

func (q *qcIterator) Next() (bool, error) {
	for {
		ok, err := q.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance qcs iterator: %w", err)
		}
		if !ok {
			return false, nil
		}
		if _, isPrimary := q.it.GetKey(); isPrimary {
			return true, nil
		}
	}
}

func (q *qcIterator) QC() (*types.FullCommitQC, error) {
	value, err := q.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read QC value: %w", err)
	}
	qc, err := types.FullCommitQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	return qc, nil
}

func (q *qcIterator) Close() error {
	if err := q.it.Close(); err != nil {
		return fmt.Errorf("failed to close qcs iterator: %w", err)
	}
	return nil
}
