package hashvault

import (
	"fmt"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

var _ controller.PrunableStore = (*HashVault)(nil)

// PruneBelow permits the hashes of blocks below blockNumber to be deleted, as far as the vault's owner is
// concerned. A hash is deleted only once PruneHistory() has permitted it too.
func (v *HashVault) PruneBelow(blockNumber uint64) {
	raiseFloor(&v.outerFloor, blockNumber)
}

// gcFilter reports whether LittDB may delete key: only a block below both floors may go.
func (v *HashVault) gcFilter(key []byte, _ bool) (bool, error) {
	blockNumber, err := decodeKey(key)
	if err != nil {
		return false, fmt.Errorf("decode a hash vault key: %w", err)
	}
	return blockNumber < min(v.outerFloor.Load(), v.gcFloor.Load()), nil
}

// raiseFloor raises floor to blockNumber, leaving it where it is when it is already higher.
func raiseFloor(floor *atomic.Uint64, blockNumber uint64) {
	for {
		current := floor.Load()
		if blockNumber <= current || floor.CompareAndSwap(current, blockNumber) {
			return
		}
	}
}

// Name implements controller.PrunableStore.
func (v *HashVault) Name() string {
	return "HashVault"
}

// PruneHistory implements controller.PrunableStore. It permits the hashes of blocks below blockNumber to be
// deleted, as far as the storage garbage collector is concerned. A hash is deleted only once PruneBelow()
// has permitted it too.
func (v *HashVault) PruneHistory(blockNumber uint64) error {
	raiseFloor(&v.gcFloor, blockNumber)
	return nil
}

// PruneSnapshots implements controller.PrunableStore. The vault keeps no snapshots.
func (v *HashVault) PruneSnapshots(uint64) error {
	return nil
}

// ExternalPruning implements controller.PrunableStore. The vault has no pruner of its own.
func (v *HashVault) ExternalPruning() bool {
	return true
}

// GetRollbackFloor implements controller.PrunableStore. Every recorded block is readable directly, so the
// floor is the newest recorded block less rollbackWindow, or 0 when the window is deeper than that.
func (v *HashVault) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, ok := v.Head()
	if !ok || head < rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock implements controller.PrunableStore. It returns the newest recorded block, or 0 when the
// vault holds no hashes.
func (v *HashVault) GetLatestBlock() (uint64, error) {
	head, _ := v.Head()
	return head, nil
}
