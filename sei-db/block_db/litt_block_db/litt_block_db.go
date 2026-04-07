package littblockdb

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"

	blockdb "github.com/sei-protocol/sei-chain/sei-db/block_db"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
)

// littBlockDB implements blockdb.BlockDB backed by LittDB.
//
// Height tracking (loHeight, hiHeight, hasBlocks) is maintained in memory only and is lost on
// restart. A future LittDB feature will be needed to persist this metadata.
type littBlockDB struct {
	db    litt.DB
	table litt.Table

	// TODO: Wire up blocksToKeep once LittDB supports the logical-clock-based pruning scheme.
	// The intended design is:
	//   - Inject a logical clock (atomic int64 nanos) as LittDB's Config.Clock
	//   - Map block height to nanos so segments seal with height-correlated timestamps
	//   - On Prune(), adjust the table TTL so GC removes old segments
	// This requires LittDB API changes (a way to update TTL cheaply per block, or a callback).
	blocksToKeep uint64

	hasBlocks atomic.Bool
	loHeight  atomic.Uint64
	hiHeight  atomic.Uint64
}

var _ blockdb.BlockDB = (*littBlockDB)(nil)

// NewLittBlockDB creates or opens a LittDB-backed BlockDB at the given path.
//
// blocksToKeep is stored but not yet used for pruning (see Prune).
func NewLittBlockDB(path string, blocksToKeep uint64) (*littBlockDB, error) {
	config, err := litt.DefaultConfig(path)
	if err != nil {
		return nil, fmt.Errorf("litt default config: %w", err)
	}

	db, err := litt.NewDB(config)
	if err != nil {
		return nil, fmt.Errorf("litt open: %w", err)
	}

	table, err := db.GetTable("blocks")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("litt get table: %w", err)
	}

	return &littBlockDB{
		db:           db,
		table:        table,
		blocksToKeep: blocksToKeep,
	}, nil
}

func (l *littBlockDB) WriteBlock(_ context.Context, block *blockdb.BinaryBlock) error {
	value, secondaryKeys := marshalBlock(block)
	if err := l.table.Put(encodeHeightKey(block.Height), value, secondaryKeys...); err != nil {
		return fmt.Errorf("litt put: %w", err)
	}

	l.updateHeights(block.Height)
	return nil
}

func (l *littBlockDB) Flush(_ context.Context) error {
	return l.table.Flush()
}

func (l *littBlockDB) GetBlockByHeight(_ context.Context, height uint64) (*blockdb.BinaryBlock, bool, error) {
	value, exists, err := l.table.Get(encodeHeightKey(height))
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	blk, err := unmarshalBlock(value)
	if err != nil {
		return nil, false, fmt.Errorf("unmarshal block at height %d: %w", height, err)
	}
	return blk, true, nil
}

func (l *littBlockDB) GetBlockByHash(_ context.Context, hash []byte) (*blockdb.BinaryBlock, bool, error) {
	value, exists, err := l.table.Get(encodeBlockHashKey(hash))
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	blk, err := unmarshalBlock(value)
	if err != nil {
		return nil, false, fmt.Errorf("unmarshal block by hash: %w", err)
	}
	return blk, true, nil
}

func (l *littBlockDB) GetTransactionByHash(_ context.Context, txHash []byte) (*blockdb.BinaryTransaction, bool, error) {
	txData, exists, err := l.table.Get(encodeTxHashKey(txHash))
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	return &blockdb.BinaryTransaction{
		Hash:        bytes.Clone(txHash),
		Transaction: bytes.Clone(txData),
	}, true, nil
}

// Prune is a no-op. Pruning requires LittDB API changes to support a logical-clock-based TTL
// scheme that maps block height to nanosecond timestamps.
//
// TODO: Implement pruning. The intended design:
//   - A blockClock (atomic int64) is injected as LittDB's Config.Clock
//   - Each block height maps to height * nanosPerBlock
//   - On Prune(lowestHeightToKeep), set table TTL = clock - lowestHeightToKeep * nanosPerBlock
//   - LittDB GC removes segments whose seal time falls outside the retention window
//   - This is segment-granular: some blocks below the prune target may linger until the entire
//     segment ages out
//   - Requires a LittDB API change to update TTL without a disk write on every block
func (l *littBlockDB) Prune(_ context.Context, _ uint64) error {
	return nil
}

// GetLowestBlockHeight returns the lowest block height written in this session.
//
// TODO: This value is lost on restart. A future LittDB feature is needed to persist it.
func (l *littBlockDB) GetLowestBlockHeight(_ context.Context) (uint64, error) {
	if !l.hasBlocks.Load() {
		return 0, blockdb.ErrNoBlocks
	}
	return l.loHeight.Load(), nil
}

// GetHighestBlockHeight returns the highest block height written in this session.
//
// TODO: This value is lost on restart. A future LittDB feature is needed to persist it.
func (l *littBlockDB) GetHighestBlockHeight(_ context.Context) (uint64, error) {
	if !l.hasBlocks.Load() {
		return 0, blockdb.ErrNoBlocks
	}
	return l.hiHeight.Load(), nil
}

func (l *littBlockDB) Close(_ context.Context) error {
	return l.db.Close()
}

func (l *littBlockDB) updateHeights(height uint64) {
	if !l.hasBlocks.Load() {
		l.loHeight.Store(height)
		l.hiHeight.Store(height)
		l.hasBlocks.Store(true)
		return
	}

	for {
		old := l.hiHeight.Load()
		if height <= old {
			break
		}
		if l.hiHeight.CompareAndSwap(old, height) {
			break
		}
	}

	for {
		old := l.loHeight.Load()
		if height >= old {
			break
		}
		if l.loHeight.CompareAndSwap(old, height) {
			break
		}
	}
}
