package receipt

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var _ ReceiptIterator = (*receiptIterator)(nil)

// receiptIterator walks a littidx store's receipts. A block is stored as a part key holding
// its receipts concatenated, each transaction a secondary key aliasing its own receipt, so the
// walk yields the secondary keys and reads the block from the part key they follow.
type receiptIterator struct {
	// it is nil for a walk with nothing to visit, which reports exhausted on the first Next.
	it litt.Iterator

	block  uint64
	txHash common.Hash
}

func (s *littReceiptStore) IterateReceipts(startBlock uint64) (ReceiptIterator, error) {
	start := startBlock
	// Litt expires bodies lazily, so blocks below the floor can still be on disk. Point reads
	// refuse them (see belowRetentionFloor) and a walk answers the same.
	if floor := s.earliestVersion.Load(); floor > 0 && start < uint64(floor) { //nolint:gosec // floor is positive
		start = uint64(floor) //nolint:gosec // floor is positive
	}

	// Above the head there is nothing to walk, and no part key to position at. Without this the
	// walk would fall through to the whole store below.
	head := s.latestVersion.Load()
	if head <= 0 || start > uint64(head) { //nolint:gosec // head is positive
		return &receiptIterator{}, nil
	}

	it, found, err := s.receipts.IteratorAt(littPartKey(start, 0), false)
	if err != nil {
		return nil, fmt.Errorf("failed to open receipt iterator at block %d: %w", start, err)
	}
	if found {
		return &receiptIterator{it: it}, nil
	}
	return s.walkFromOldest(start)
}

// walkFromOldest begins a walk at the oldest data litt holds, positioned at the first block at or
// above start. It reports an exhausted walk when litt holds no such block.
func (s *littReceiptStore) walkFromOldest(start uint64) (ReceiptIterator, error) {
	it, err := s.receipts.Iterator(false)
	if err != nil {
		return nil, fmt.Errorf("failed to open receipt iterator: %w", err)
	}

	// Seeking forward is what keeps pruned receipts out of the walk. Positioning by key asks litt's
	// keymap, while a walk from the oldest data reads segment files, and garbage collection deletes
	// a segment's keys from the keymap before it advances the line marking the oldest readable
	// segment. A prune landing in that interval leaves the part key for start missing from the
	// keymap — which is how the walk arrived here — while its segment is still readable. Without
	// this seek the walk would yield receipts from below the retention floor, which a point read
	// refuses to serve.
	block, found, err := seekBlock(it, start)
	if err != nil {
		return nil, errors.Join(err, it.Close())
	}
	if !found {
		if closeErr := it.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to close receipt iterator: %w", closeErr)
		}
		return &receiptIterator{}, nil
	}
	return &receiptIterator{it: it, block: block}, nil
}

// seekBlock advances it to the part key of the first block at or above start, reporting the block
// that part belongs to. found is false once the walk is exhausted with no such block.
func seekBlock(it litt.Iterator, start uint64) (block uint64, found bool, err error) {
	for {
		ok, err := it.Next()
		if err != nil {
			return 0, false, fmt.Errorf("failed to advance receipt iterator: %w", err)
		}
		if !ok {
			return 0, false, nil
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return 0, false, fmt.Errorf("failed to read receipt key: %w", err)
		}
		if !isPrimary {
			continue
		}
		at, err := decodePartKeyHeight(key)
		if err != nil {
			return 0, false, err
		}
		if at >= start {
			return at, true, nil
		}
	}
}

func (r *receiptIterator) Next() (bool, error) {
	if r.it == nil {
		return false, nil
	}
	for {
		ok, err := r.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance receipt iterator: %w", err)
		}
		if !ok {
			return false, nil
		}

		key, isPrimary, err := r.it.GetKey()
		if err != nil {
			return false, fmt.Errorf("failed to read receipt key: %w", err)
		}
		if isPrimary {
			// A part key holds a whole block rather than one receipt; it names the block the
			// secondary keys after it belong to.
			r.block, err = decodePartKeyHeight(key)
			if err != nil {
				return false, err
			}
			continue
		}
		r.txHash = common.BytesToHash(key)
		return true, nil
	}
}

func (r *receiptIterator) BlockNumber() uint64 {
	return r.block
}

func (r *receiptIterator) TxHash() common.Hash {
	return r.txHash
}

func (r *receiptIterator) Receipt() (*types.Receipt, error) {
	value, err := r.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read receipt %s: %w", r.txHash, err)
	}
	data, err := decodeReceiptData(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode receipt %s: %w", r.txHash, err)
	}
	// gogoproto Unmarshal copies all byte and string fields, so it is safe over litt's shared buffer.
	var parsed types.Receipt
	if err := parsed.Unmarshal(data.Body); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt %s: %w", r.txHash, err)
	}
	return &parsed, nil
}

func (r *receiptIterator) Close() error {
	if r.it == nil {
		return nil
	}
	if err := r.it.Close(); err != nil {
		return fmt.Errorf("failed to close receipt iterator: %w", err)
	}
	return nil
}
