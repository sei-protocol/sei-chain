package receipt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// TxHashIndex maps transaction hashes to the block number that contains them.
// Implementations must be safe for concurrent use.
type TxHashIndex interface {
	// GetBlockNumber returns the block number for a given tx hash.
	// Returns (0, false, nil) when the hash is not indexed.
	GetBlockNumber(ctx context.Context, txHash common.Hash) (blockNumber uint64, ok bool, err error)

	// IndexBlock associates every tx hash in the slice with blockNumber.
	IndexBlock(ctx context.Context, blockNumber uint64, txHashes []common.Hash) error

	// PruneBefore removes all index entries for blocks strictly below blockNumber.
	PruneBefore(ctx context.Context, blockNumber uint64) error

	Close() error
}

// Key layout for the Pebble-backed index:
//
//	Primary:  'h' + txHash (32 bytes)  -> blockNumber (8 bytes big-endian)
//	Reverse:  'b' + blockNumber (8 BE) + txHash (32 bytes) -> empty
//
// The reverse mapping lets PruneBefore delete old entries efficiently
// using a range scan on the 'b' prefix.
const (
	txHashPrefix = 'h'
	blockPrefix  = 'b'
	txHashLen    = 32
	blockNumLen  = 8
)

func makeTxHashKey(txHash common.Hash) []byte {
	key := make([]byte, 1+txHashLen)
	key[0] = txHashPrefix
	copy(key[1:], txHash[:])
	return key
}

func makeBlockPrefixKey(blockNumber uint64) []byte {
	key := make([]byte, 1+blockNumLen)
	key[0] = blockPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	return key
}

func makeBlockTxKey(blockNumber uint64, txHash common.Hash) []byte {
	key := make([]byte, 1+blockNumLen+txHashLen)
	key[0] = blockPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	copy(key[1+blockNumLen:], txHash[:])
	return key
}

func encodeBlockNumber(blockNumber uint64) []byte {
	buf := make([]byte, blockNumLen)
	binary.BigEndian.PutUint64(buf, blockNumber)
	return buf
}

func decodeBlockNumber(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

// PebbleTxHashIndex is the first concrete implementation of TxHashIndex,
// backed by the shared sei-db Pebble KV wrapper.
type PebbleTxHashIndex struct {
	db        dbtypes.KeyValueDB
	closeOnce sync.Once
}

var _ TxHashIndex = (*PebbleTxHashIndex)(nil)

// NewPebbleTxHashIndex opens (or creates) a Pebble-backed tx hash index
// in the given directory.
func NewPebbleTxHashIndex(dir string) (*PebbleTxHashIndex, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create tx hash index directory: %w", err)
	}
	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = dir
	db, err := pebbledb.Open(context.Background(), &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open tx hash index pebble db: %w", err)
	}
	return &PebbleTxHashIndex{db: db}, nil
}

func (idx *PebbleTxHashIndex) GetBlockNumber(_ context.Context, txHash common.Hash) (uint64, bool, error) {
	val, err := idx.db.Get(makeTxHashKey(txHash))
	if err != nil {
		if errorutils.IsNotFound(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(val) < blockNumLen {
		return 0, false, fmt.Errorf("corrupt tx hash index entry: expected %d bytes, got %d", blockNumLen, len(val))
	}
	return decodeBlockNumber(val), true, nil
}

func (idx *PebbleTxHashIndex) IndexBlock(_ context.Context, blockNumber uint64, txHashes []common.Hash) (err error) {
	if len(txHashes) == 0 {
		return nil
	}
	batch := idx.db.NewBatch()
	defer func() {
		if closeErr := batch.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	blockVal := encodeBlockNumber(blockNumber)
	for _, txHash := range txHashes {
		if err := batch.Set(makeTxHashKey(txHash), blockVal); err != nil {
			return err
		}
		if err := batch.Set(makeBlockTxKey(blockNumber, txHash), nil); err != nil {
			return err
		}
	}
	// Avoid Sync on every block: fsync per commit would add large latency;
	// Pebble still appends to the WAL without forcing a full sync each time.
	return batch.Commit(dbtypes.WriteOptions{})
}

func (idx *PebbleTxHashIndex) PruneBefore(_ context.Context, blockNumber uint64) (err error) {
	lowerBound := makeBlockPrefixKey(0)
	upperBound := makeBlockPrefixKey(blockNumber)

	iter, err := idx.db.NewIter(&dbtypes.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	batch := idx.db.NewBatch()
	defer func() {
		if closeErr := batch.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	const maxBatchSize = 10000
	count := 0

	for ; iter.Valid(); iter.Next() {
		key := bytes.Clone(iter.Key())
		if len(key) == 1+blockNumLen+txHashLen && key[0] == blockPrefix {
			var txHash common.Hash
			copy(txHash[:], key[1+blockNumLen:])
			txHashKey := makeTxHashKey(txHash)
			// Only delete the primary key if it still points to this block.
			// An overwrite (same tx hash re-indexed at a newer block) would
			// have updated the primary key but left the old reverse entry;
			// blindly deleting the primary key would corrupt the newer mapping.
			primaryVal, getErr := idx.db.Get(txHashKey)
			if getErr != nil && !errorutils.IsNotFound(getErr) {
				return getErr
			}
			if getErr == nil && decodeBlockNumber(primaryVal) == decodeBlockNumber(key[1:1+blockNumLen]) {
				if err := batch.Delete(txHashKey); err != nil {
					return err
				}
			}
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		count++
		if count >= maxBatchSize {
			if err := batch.Commit(dbtypes.WriteOptions{}); err != nil {
				return err
			}
			batch.Reset()
			count = 0
		}
	}
	if err = iter.Error(); err != nil {
		return err
	}
	if count > 0 {
		return batch.Commit(dbtypes.WriteOptions{})
	}
	return nil
}

func (idx *PebbleTxHashIndex) Close() error {
	var err error
	idx.closeOnce.Do(func() {
		err = idx.db.Close()
	})
	return err
}

// TxHashIndexDir returns the canonical subdirectory name for the Pebble
// tx-hash index within a receipt store DB directory.
func TxHashIndexDir(receiptDBDir string) string {
	return filepath.Join(receiptDBDir, "tx-hash-index")
}
