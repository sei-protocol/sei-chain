package vault

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ HashVault = (*StandardHashVault)(nil)

// StandardHashVault is a HashVault stored in a seiwal WAL indexed by block height.
type StandardHashVault struct {
	wal seiwal.WAL[[]byte]

	// The EVM chain ID of the record of block gigaActivationHeight-1.
	chainID uint64

	// The first block whose app hash is computed.
	gigaActivationHeight uint64

	// The highest block height the vault knows. Advanced by Append().
	head atomic.Uint64
}

// Opens the vault in config.Path, creating it if absent. Errors if gigaActivationHeight is 0.
func NewStandardHashVault(
	// Configures the WAL holding the records. PermitGaps is ignored: the vault always requires consecutive block
	// heights.
	config seiwal.Config,
	// The EVM chain ID of the chain the records belong to.
	chainID uint64,
	// The first block whose app hash is computed. The vault knows block gigaActivationHeight-1, whose hashes are
	// all zeros, without storing it.
	gigaActivationHeight uint64,
) (*StandardHashVault, error) {
	if gigaActivationHeight == 0 {
		return nil, fmt.Errorf("giga activation height must be at least 1")
	}
	// Append() and Iterator() rely on the WAL rejecting any height that does not follow the highest stored one.
	config.PermitGaps = false
	if err := os.MkdirAll(config.Path, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create hash vault directory %q: %w", config.Path, err)
	}
	wal, err := seiwal.NewWAL(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to open hash vault WAL in %q: %w", config.Path, err)
	}
	v := &StandardHashVault{wal: wal, chainID: chainID, gigaActivationHeight: gigaActivationHeight}
	_, highest, err := v.Bounds()
	if err != nil {
		_ = wal.Close()
		return nil, fmt.Errorf("failed to read the hash vault head in %q: %w", config.Path, err)
	}
	v.head.Store(highest)
	return v, nil
}

// GetBounds returns what Bounds() would for the vault in config.Path, without opening the vault. It recovers the
// vault's files as opening the vault would, and fails while the vault is open or if gigaActivationHeight is 0.
func GetBounds(
	// Configures the vault's WAL.
	config seiwal.Config,
	// The first block whose app hash is computed.
	gigaActivationHeight uint64,
) (lowest uint64, highest uint64, err error) {
	if gigaActivationHeight == 0 {
		return 0, 0, fmt.Errorf("giga activation height must be at least 1")
	}
	stored, storedLowest, storedHighest, err := seiwal.GetRange(config.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read the hash vault range in %q: %w", config.Path, err)
	}
	lowest, highest = knownRange(stored, storedLowest, storedHighest, gigaActivationHeight)
	return lowest, highest, nil
}

// Returns the lowest and highest block heights a vault knows from the range of its stored records. Block
// gigaActivationHeight-1 is known without being stored, so it is the whole range of an empty vault, and the bottom
// of a range that starts at gigaActivationHeight.
func knownRange(
	// Whether any record is stored.
	stored bool,
	// The lowest stored block height. Read only if stored.
	storedLowest uint64,
	// The highest stored block height. Read only if stored.
	storedHighest uint64,
	// The first block whose app hash is computed.
	gigaActivationHeight uint64,
) (lowest uint64, highest uint64) {
	zeroHashHeight := gigaActivationHeight - 1
	if !stored {
		return zeroHashHeight, zeroHashHeight
	}
	if storedLowest == gigaActivationHeight {
		return zeroHashHeight, storedHighest
	}
	return storedLowest, storedHighest
}

func (v *StandardHashVault) Append(records []*apphash.AppHashData) error {
	for _, record := range records {
		data, err := serializeRecord(record)
		if err != nil {
			return fmt.Errorf("failed to encode hash vault record for block %d: %w",
				record.BlockHeight(), err)
		}
		if err := v.wal.Append(record.BlockHeight(), data); err != nil {
			return fmt.Errorf("failed to append hash vault record for block %d: %w",
				record.BlockHeight(), err)
		}
	}
	if err := v.wal.Flush(); err != nil {
		return fmt.Errorf("failed to flush hash vault: %w", err)
	}
	if len(records) > 0 {
		v.head.Store(records[len(records)-1].BlockHeight())
	}
	return nil
}

func (v *StandardHashVault) Bounds() (uint64, uint64, error) {
	stored, storedLowest, storedHighest, err := v.wal.Bounds()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read hash vault bounds: %w", err)
	}
	lowest, highest := knownRange(stored, storedLowest, storedHighest, v.gigaActivationHeight)
	return lowest, highest, nil
}

func (v *StandardHashVault) Iterator(start uint64, end uint64) (apphash.AppHashIterator, error) {
	if start == v.gigaActivationHeight-1 {
		it, err := v.zeroHashIterator(end)
		if err != nil {
			return nil, fmt.Errorf("failed to iterate hash vault from block %d to %d: %w", start, end, err)
		}
		return it, nil
	}
	it, err := v.storedRecordIterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate hash vault from block %d to %d: %w", start, end, err)
	}
	return it, nil
}

// Returns an iterator over the record of block gigaActivationHeight-1 followed by the stored records through end.
func (v *StandardHashVault) zeroHashIterator(end uint64) (*hashVaultIterator, error) {
	zeroHash := apphash.NewZeroAppHashData(v.chainID, v.gigaActivationHeight)
	if end == zeroHash.BlockHeight() {
		lowest, _, err := v.Bounds()
		if err != nil {
			return nil, err
		}
		if lowest != zeroHash.BlockHeight() {
			return nil, fmt.Errorf("block %d is pruned, the lowest block known is %d", end, lowest)
		}
		return newZeroHashIterator(zeroHash), nil
	}
	it, err := v.storedRecordIterator(v.gigaActivationHeight, end)
	if err != nil {
		return nil, err
	}
	it.Prepend(zeroHash)
	return it, nil
}

// Returns an iterator over the stored records from start through end. Errors unless a record is stored at start.
func (v *StandardHashVault) storedRecordIterator(start uint64, end uint64) (*hashVaultIterator, error) {
	inner, err := v.wal.Iterator(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL iterator: %w", err)
	}
	return newHashVaultIterator(inner, start)
}

func (v *StandardHashVault) Name() string {
	return "AppHashVault"
}

// ExternalPruning is unconditionally true: the vault prunes only when told to.
func (v *StandardHashVault) ExternalPruning() bool {
	return true
}

// PruneHistory schedules removal of the records below blockNumber, capped at the head so the newest record is kept.
// It does not wait for the removal.
func (v *StandardHashVault) PruneHistory(blockNumber uint64) error {
	target := min(blockNumber, v.head.Load())
	if err := v.wal.PruneBefore(target); err != nil {
		return fmt.Errorf("failed to prune hash vault below block %d: %w", target, err)
	}
	return nil
}

// PruneSnapshots does nothing: the vault keeps no snapshots.
func (v *StandardHashVault) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, where head is GetLatestBlock(). It returns 0 when the window is
// deeper than the head.
func (v *StandardHashVault) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head := v.head.Load()
	if head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the highest block height the vault knows, as Bounds() does.
func (v *StandardHashVault) GetLatestBlock() (uint64, error) {
	return v.head.Load(), nil
}

func (v *StandardHashVault) Close() error {
	if err := v.wal.Close(); err != nil {
		return fmt.Errorf("failed to close hash vault: %w", err)
	}
	return nil
}
