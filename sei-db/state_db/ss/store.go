package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

type BackendType string

const (
	// RocksDBBackend represents rocksdb
	// - use rocksdb build tag
	RocksDBBackend BackendType = "rocksdb"

	// PebbleDBBackend represents pebbledb
	PebbleDBBackend BackendType = "pebbledb"
)

type BackendInitializer func(dir string, config config.StateStoreConfig) (types.StateStore, error)

var backends = map[BackendType]BackendInitializer{}

func RegisterBackend(backendType BackendType, initializer BackendInitializer) {
	backends[backendType] = initializer
}

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// When WriteMode/ReadMode are both cosmos_only (the default), the EVM stores are not
// opened and the composite store behaves identically to a plain state store.
func NewStateStore(logger logger.Logger, homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	return composite.NewCompositeStateStore(ssConfig, homeDir, logger)
}

// RecoverStateStore replays WAL entries that the state store hasn't applied yet.
// Uses the shared ReplayWAL implementation to avoid duplication with composite recovery.
func RecoverStateStore(logger logger.Logger, changelogPath string, stateStore types.StateStore) error {
	ssLatestVersion := stateStore.GetLatestVersion()
	logger.Info(fmt.Sprintf("Recovering from changelog %s with latest SS version %d", changelogPath, ssLatestVersion))

	return composite.ReplayWAL(logger, changelogPath, ssLatestVersion, -1, func(entry proto.ChangelogEntry) error {
		if err := stateStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
			return err
		}
		return stateStore.SetLatestVersion(entry.Version)
	})
}
