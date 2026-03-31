package parquet

import (
	"encoding/binary"
	"fmt"
	"path/filepath"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
)

// TxIndex is a lightweight pebble-backed index mapping tx_hash -> block_number.
// It allows GetReceiptByTxHash to narrow the parquet file search to a single file
// instead of scanning all files.
type TxIndex struct {
	db *pebble.DB
}

func OpenTxIndex(baseDir string) (*TxIndex, error) {
	dir := filepath.Join(baseDir, "tx-index")
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open tx index: %w", err)
	}
	return &TxIndex{db: db}, nil
}

func (idx *TxIndex) Close() error {
	if idx == nil || idx.db == nil {
		return nil
	}
	return idx.db.Close()
}

// SetBatch writes a batch of tx_hash -> block_number mappings.
func (idx *TxIndex) SetBatch(entries []TxIndexEntry) error {
	if idx == nil {
		return nil
	}
	batch := idx.db.NewBatch()
	defer func() { _ = batch.Close() }()

	val := make([]byte, 8)
	for _, e := range entries {
		binary.BigEndian.PutUint64(val, e.BlockNumber)
		if err := batch.Set(e.TxHash[:], val, pebble.NoSync); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.NoSync)
}

// GetBlockNumber returns the block number for a tx hash, or 0, false if not found.
func (idx *TxIndex) GetBlockNumber(txHash common.Hash) (uint64, bool) {
	if idx == nil {
		return 0, false
	}
	val, closer, err := idx.db.Get(txHash[:])
	if err != nil {
		return 0, false
	}
	defer func() { _ = closer.Close() }()
	if len(val) < 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(val), true
}

// PruneBefore deletes all entries with block_number < pruneBlock.
// This is a full scan since pebble keys are tx hashes (not ordered by block).
// Intended to run infrequently on the prune interval.
func (idx *TxIndex) PruneBefore(pruneBlock uint64) error {
	if idx == nil {
		return nil
	}
	batch := idx.db.NewBatch()
	defer func() { _ = batch.Close() }()

	iter, err := idx.db.NewIter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	for valid := iter.First(); valid; valid = iter.Next() {
		val := iter.Value()
		if len(val) < 8 {
			continue
		}
		blockNum := binary.BigEndian.Uint64(val)
		if blockNum < pruneBlock {
			if err := batch.Delete(iter.Key(), pebble.NoSync); err != nil {
				return err
			}
		}
	}
	return batch.Commit(pebble.NoSync)
}

type TxIndexEntry struct {
	TxHash      common.Hash
	BlockNumber uint64
}
