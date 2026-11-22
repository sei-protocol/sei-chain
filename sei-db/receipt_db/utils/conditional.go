// Package utils holds helpers shared by the receipt store implementations.
package utils

import (
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
)

// FilterExistingReceipts omits conditional records whose hash is already stored, appears
// earlier in records, or has an unconditional record anywhere in records. Callers must
// serialize this check with their writes.
func FilterExistingReceipts(records []receipttypes.ReceiptRecord, exists func(common.Hash) (bool, error)) ([]receipttypes.ReceiptRecord, error) {
	if !slices.ContainsFunc(records, func(record receipttypes.ReceiptRecord) bool { return record.KeepExisting }) {
		return records, nil
	}
	executed := make(map[common.Hash]struct{}, len(records))
	for _, record := range records {
		if record.Receipt != nil && !record.KeepExisting {
			executed[record.TxHash] = struct{}{}
		}
	}
	filtered := make([]receipttypes.ReceiptRecord, 0, len(records))
	seen := make(map[common.Hash]struct{}, len(records))
	for _, record := range records {
		if record.Receipt == nil {
			continue
		}
		_, repeated := seen[record.TxHash]
		seen[record.TxHash] = struct{}{}
		if record.KeepExisting {
			if repeated {
				continue
			}
			if _, ok := executed[record.TxHash]; ok {
				continue
			}
			found, err := exists(record.TxHash)
			if err != nil {
				return nil, fmt.Errorf("check existing receipt %s: %w", record.TxHash, err)
			}
			if found {
				continue
			}
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}
