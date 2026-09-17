package receipt

import (
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
	if !found {
		// Possible if GC deleted the block out from under us. If this happens, just start at begining of data.
		it, err = s.receipts.Iterator(false)
		if err != nil {
			return nil, fmt.Errorf("failed to open receipt iterator: %w", err)
		}
	}
	return &receiptIterator{it: it}, nil
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
