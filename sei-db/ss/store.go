package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/pruning"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/sei-protocol/sei-db/stream/changelog"
)

type BackendType string

const (
	// RocksDBBackend represents rocksdb
	// - use rocksdb build tag
	RocksDBBackend BackendType = "rocksdb"

	// PebbleDBBackend represents pebbledb
	PebbleDBBackend BackendType = "pebbledb"

	// SQLiteBackend represents sqlite
	SQLiteBackend BackendType = "sqlite"
)

type BackendInitializer func(dir string, config config.StateStoreConfig) (types.StateStore, error)

var backends = map[BackendType]BackendInitializer{}

func RegisterBackend(backendType BackendType, initializer BackendInitializer) {
	backends[backendType] = initializer
}

// NewStateStore Create a new state store with the specified backend type
func NewStateStore(logger logger.Logger, homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	initializer, ok := backends[BackendType(ssConfig.Backend)]
	if !ok {
		return nil, fmt.Errorf("unsupported backend: %s", ssConfig.Backend)
	}
	stateStore, err := initializer(homeDir, ssConfig)
	if err != nil {
		return nil, err
	}
	// Handle auto recovery for DB running with async mode
	if ssConfig.DedicatedChangelog {
		changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(homeDir, ssConfig.Backend))
		if ssConfig.DBDirectory != "" {
			changelogPath = utils.GetChangelogPath(ssConfig.DBDirectory)
		}
		err := RecoverStateStore(logger, changelogPath, stateStore)
		if err != nil {
			return nil, err
		}
	}
	// Start the pruning manager for DB
	pruningManager := pruning.NewPruningManager(logger, stateStore, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds))
	pruningManager.Start()
	return stateStore, nil
}

// RecoverStateStore will be called during initialization to recover the state from rlog
func RecoverStateStore(logger logger.Logger, changelogPath string, stateStore types.StateStore) error {
	ssLatestVersion, err := stateStore.GetLatestVersion()
	logger.Info(fmt.Sprintf("Recovering from changelog %s at latest SS version %d", changelogPath, ssLatestVersion))
	if err != nil {
		return err
	}
	if ssLatestVersion <= 0 {
		return nil
	}
	streamHandler, err := changelog.NewStream(logger, changelogPath, changelog.Config{})
	if err != nil {
		return err
	}
	firstOffset, errFirst := streamHandler.FirstOffset()
	if firstOffset <= 0 || errFirst != nil {
		return err
	}
	lastOffset, errLast := streamHandler.LastOffset()
	if lastOffset <= 0 || errLast != nil {
		return err
	}
	lastEntry, errRead := streamHandler.ReadAt(lastOffset)
	if errRead != nil {
		return err
	}
	// Look backward to find where we should start replay from
	curVersion := lastEntry.Version
	curOffset := lastOffset
	for curVersion > ssLatestVersion && curOffset > firstOffset {
		curOffset--
		curEntry, errRead := streamHandler.ReadAt(curOffset)
		if errRead != nil {
			return err
		}
		curVersion = curEntry.Version
	}
	// Replay from the offset where the offset where the version is larger than SS store latest version
	targetStartOffset := curOffset
	logger.Info(fmt.Sprintf("Start replaying changelog to recover StateStore from offset %d to %d", targetStartOffset, lastOffset))
	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			// commit to state store
			for _, cs := range entry.Changesets {
				if err := stateStore.ApplyChangeset(entry.Version, cs); err != nil {
					return err
				}
			}
			if err := stateStore.SetLatestVersion(entry.Version); err != nil {
				return err
			}
			return nil
		})
	}
	return nil
}
