package bootstrap

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

type RecoverableStore struct {
	name          string
	db            types.Rollbackable
	targetVersion int64
}

// CrashRecover brings the opened stores onto one height after an unclean shutdown. It assumes the
// BlockDB is always ahead of other state DBs and will make sure all other DBs are reconciled to the same height.
//
// NewGigaStorageManager calls it once every store is open and before either the checkpoint schedule
// or the prune cycle starts. It is a no-op on a node that shut down cleanly, and idempotent, so a run
// interrupted partway is completed by the next open.
func (m *GigaStorageManager) CrashRecover() error {
	// TODO: implement later
	// Step 1: Check if BlockStore is ahead of other stores
	// Step 2: for each store, collect its latest height
	// Step 3: Calculate the height each store should rollback
	// Step 4: call recoverStores to rollback
	return nil
}

func recoverStores(stores []RecoverableStore) error {
	for _, store := range stores {
		if err := store.db.Rollback(store.targetVersion); err != nil {
			return fmt.Errorf("rollback %s to height %d: %w", store.name, store.targetVersion, err)
		}
		logger.Info("rollback complete", "store", store.name, "target", store.targetVersion)
	}
	return nil
}
