package receipt

import (
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
)

// FilterExistingReceipts omits conditional records whose hash is already stored, appears
// earlier in records, or has an unconditional record anywhere in records. Callers must
// serialize this check with their writes.
func FilterExistingReceipts(records []ReceiptRecord, exists func(common.Hash) (bool, error)) ([]ReceiptRecord, error) {
	if !slices.ContainsFunc(records, func(record ReceiptRecord) bool { return record.KeepExisting }) {
		return records, nil
	}
	executed := make(map[common.Hash]struct{}, len(records))
	for _, record := range records {
		if record.Receipt != nil && !record.KeepExisting {
			executed[record.TxHash] = struct{}{}
		}
	}
	filtered := make([]ReceiptRecord, 0, len(records))
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
