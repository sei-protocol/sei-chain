package vault

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ HashVault = (*StandardHashVault)(nil)

// StandardHashVault is a HashVault stored in a seiwal WAL indexed by block height.
type StandardHashVault struct {
	wal seiwal.WAL[[]byte]
}

// Opens the vault in config.Path, creating it if absent. config.PermitGaps is ignored: the vault always requires
// consecutive block heights.
func NewStandardHashVault(config seiwal.Config) (*StandardHashVault, error) {
	// Append() and Iterator() rely on the WAL rejecting any height that does not follow the highest stored one.
	config.PermitGaps = false
	if err := os.MkdirAll(config.Path, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create hash vault directory %q: %w", config.Path, err)
	}
	wal, err := seiwal.NewWAL(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to open hash vault WAL in %q: %w", config.Path, err)
	}
	return &StandardHashVault{wal: wal}, nil
}

func (v *StandardHashVault) Append(records []*apphash.AppHashData) error {
	for _, record := range records {
		if err := v.wal.Append(record.BlockHeight(), serializeRecord(record)); err != nil {
			return fmt.Errorf("failed to append hash vault record for block %d: %w",
				record.BlockHeight(), err)
		}
	}
	if err := v.wal.Flush(); err != nil {
		return fmt.Errorf("failed to flush hash vault: %w", err)
	}
	return nil
}

func (v *StandardHashVault) Bounds() (bool, uint64, uint64, error) {
	ok, lowest, highest, err := v.wal.Bounds()
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to read hash vault bounds: %w", err)
	}
	return ok, lowest, highest, nil
}

func (v *StandardHashVault) Iterator(start uint64, end uint64) (apphash.AppHashIterator, error) {
	ok, lowest, _, err := v.Bounds()
	if err != nil {
		return nil, fmt.Errorf("failed to check iterator range: %w", err)
	}
	// The WAL starts an iterator whose start is below its first record at that first record instead of failing.
	if !ok || start < lowest {
		return nil, fmt.Errorf("block %d is below the lowest stored hash vault record", start)
	}
	inner, err := v.wal.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate hash vault from block %d to %d: %w", start, end, err)
	}
	return newHashVaultIterator(inner), nil
}

func (v *StandardHashVault) Prune(blockHeight uint64) error {
	if err := v.wal.PruneBefore(blockHeight); err != nil {
		return fmt.Errorf("failed to prune hash vault below block %d: %w", blockHeight, err)
	}
	return nil
}

func (v *StandardHashVault) Close() error {
	if err := v.wal.Close(); err != nil {
		return fmt.Errorf("failed to close hash vault: %w", err)
	}
	return nil
}
