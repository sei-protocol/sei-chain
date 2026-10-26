package evmonly

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	// queuedMemoryReceiptQueue is the number of blocks the writer may fall behind before
	// SetReceipts waits.
	queuedMemoryReceiptQueue = 64
	// queuedMemoryReceiptRetention is the number of most recent blocks whose receipts are kept.
	queuedMemoryReceiptRetention = 256
)

var _ receipt.ReceiptStore = (*QueuedMemoryReceiptStore)(nil)

// QueuedMemoryReceiptStore is a receipt store that keeps marshaled receipts of the most recent
// blocks in process memory. Like the litt store it accepts a block's receipts onto a queue, and a
// writer marshals them, concatenates them into one block value and indexes each transaction hash
// to its range. Queries other than a receipt lookup are answered by an empty MemoryReceiptStore.
// Nothing it holds survives the process; it is for benchmarks.
type QueuedMemoryReceiptStore struct {
	*MemoryReceiptStore

	writes chan queuedMemoryReceiptWrite
	done   chan struct{}
	closed atomic.Bool

	mu       sync.RWMutex
	latest   int64
	blocks   map[uint64][]byte
	byTxHash map[common.Hash]queuedMemoryReceiptLocation
	failure  error
}

type queuedMemoryReceiptWrite struct {
	height  int64
	records []receipt.ReceiptRecord
}

type queuedMemoryReceiptLocation struct {
	block  uint64
	offset uint32
	length uint32
}

// NewQueuedMemoryReceiptStore returns an empty store with its writer running.
func NewQueuedMemoryReceiptStore() *QueuedMemoryReceiptStore {
	s := &QueuedMemoryReceiptStore{
		MemoryReceiptStore: NewMemoryReceiptStore(),
		writes:             make(chan queuedMemoryReceiptWrite, queuedMemoryReceiptQueue),
		done:               make(chan struct{}),
		blocks:             map[uint64][]byte{},
		byTxHash:           map[common.Hash]queuedMemoryReceiptLocation{},
	}
	go s.writeLoop()
	return s
}

// SetReceipts queues the block's receipts for the writer, waiting only when the queue is full.
func (s *QueuedMemoryReceiptStore) SetReceipts(ctx sdk.Context, records []receipt.ReceiptRecord) error {
	if s.closed.Load() {
		return gigareceipt.ErrStoreClosed
	}
	s.mu.RLock()
	failure := s.failure
	s.mu.RUnlock()
	if failure != nil {
		return failure
	}
	s.writes <- queuedMemoryReceiptWrite{height: ctx.BlockHeight(), records: records}
	return nil
}

func (s *QueuedMemoryReceiptStore) writeLoop() {
	defer close(s.done)
	for write := range s.writes {
		if err := s.writeBlock(write); err != nil {
			s.mu.Lock()
			if s.failure == nil {
				s.failure = err
			}
			s.mu.Unlock()
		}
	}
}

// writeBlock marshals one block's receipts into a single value, framing each as the litt store
// does with the block number and the receipt length ahead of the body.
func (s *QueuedMemoryReceiptStore) writeBlock(write queuedMemoryReceiptWrite) error {
	if write.height < 0 {
		return fmt.Errorf("receipt block height must not be negative: %d", write.height)
	}
	block := uint64(write.height)
	s.mu.RLock()
	records, err := receipt.FilterExistingReceipts(write.records, func(hash common.Hash) (bool, error) {
		_, found := s.byTxHash[hash]
		return found, nil
	})
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	value := make([]byte, 0)
	locations := make([]queuedMemoryReceiptLocation, len(records))
	for i, record := range records {
		if record.Receipt == nil {
			return errors.New("receipt record has no receipt")
		}
		body := record.ReceiptBytes
		if len(body) == 0 {
			body, err = record.Receipt.Marshal()
			if err != nil {
				return fmt.Errorf("marshal receipt %s: %w", record.TxHash, err)
			}
		}
		offset := len(value)
		value = binary.BigEndian.AppendUint64(value, block)
		value = binary.BigEndian.AppendUint32(value, uint32(len(body))) //nolint:gosec // receipts fit in uint32
		value = append(value, body...)
		locations[i] = queuedMemoryReceiptLocation{
			block:  block,
			offset: uint32(offset),              //nolint:gosec // block values fit in uint32
			length: uint32(len(value) - offset), //nolint:gosec // receipts fit in uint32
		}
	}
	_ = gigareceipt.ComputeBlockStats(records, gigareceipt.DefaultRewardPercentiles)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks[block] = value
	for i, record := range records {
		s.byTxHash[record.TxHash] = locations[i]
	}
	if block > queuedMemoryReceiptRetention {
		s.pruneBlockLocked(block - queuedMemoryReceiptRetention)
	}
	if write.height > s.latest {
		s.latest = write.height
	}
	return nil
}

// pruneBlockLocked drops a block's value and the index entries that point into it.
func (s *QueuedMemoryReceiptStore) pruneBlockLocked(block uint64) {
	value, ok := s.blocks[block]
	if !ok {
		return
	}
	delete(s.blocks, block)
	for offset := 0; offset < len(value); {
		length := int(binary.BigEndian.Uint32(value[offset+8:]))
		body := value[offset+12 : offset+12+length]
		var stored evmtypes.Receipt
		if err := stored.Unmarshal(body); err == nil {
			delete(s.byTxHash, common.HexToHash(stored.TxHashHex))
		}
		offset += 12 + length
	}
}

// LatestVersion returns the highest block the writer has stored.
func (s *QueuedMemoryReceiptStore) LatestVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// GetReceipt unmarshals the stored receipt for txHash.
func (s *QueuedMemoryReceiptStore) GetReceipt(_ sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error) {
	s.mu.RLock()
	location, ok := s.byTxHash[txHash]
	var framed []byte
	if ok {
		framed = s.blocks[location.block][location.offset : location.offset+location.length]
	}
	s.mu.RUnlock()
	if !ok {
		return nil, receipt.ErrNotFound
	}
	var stored evmtypes.Receipt
	if err := stored.Unmarshal(framed[12:]); err != nil {
		return nil, err
	}
	return &stored, nil
}

// GetReceiptFromStore is GetReceipt: this store has no legacy fallback.
func (s *QueuedMemoryReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*evmtypes.Receipt, error) {
	return s.GetReceipt(ctx, txHash)
}

// Close drains the queue and reports the first write that failed.
func (s *QueuedMemoryReceiptStore) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	close(s.writes)
	<-s.done
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failure
}
