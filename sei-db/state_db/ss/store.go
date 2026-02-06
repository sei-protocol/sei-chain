package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
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

// NewStateStore creates a new state store with the specified backend type.
// For backward compatibility - use NewStateStoreWithEVM when EVM optimization is needed.
func NewStateStore(logger logger.Logger, homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	return NewStateStoreWithEVM(logger, homeDir, ssConfig, nil)
}

// NewStateStoreWithEVM creates a new state store, optionally with EVM optimization layer.
// When evmConfig is enabled, returns a CompositeStateStore that routes EVM data to
// optimized separate databases while maintaining full compatibility with the StateStore interface.
// When evmConfig is nil or disabled, returns a plain state store.
func NewStateStoreWithEVM(
	logger logger.Logger,
	homeDir string,
	ssConfig config.StateStoreConfig,
	evmConfig *config.EVMStateStoreConfig,
) (types.StateStore, error) {
	// If EVM is enabled, use CompositeStateStore which handles everything internally
	// (creates cosmos store, EVM stores, recovery, and pruning)
	if evmConfig != nil && evmConfig.Enable {
		return composite.NewCompositeStateStore(ssConfig, *evmConfig, homeDir, logger)
	}

	// Otherwise, create plain state store with standard recovery and pruning
	return newPlainStateStore(logger, homeDir, ssConfig)
}

// newPlainStateStore creates a plain state store without EVM optimization
func newPlainStateStore(logger logger.Logger, homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	initializer, ok := backends[BackendType(ssConfig.Backend)]
	if !ok {
		return nil, fmt.Errorf("unsupported backend: %s", ssConfig.Backend)
	}
	stateStore, err := initializer(homeDir, ssConfig)
	if err != nil {
		return nil, err
	}

	// Handle auto recovery for DB running with async mode
	changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(homeDir, ssConfig.Backend))
	if ssConfig.DBDirectory != "" {
		changelogPath = utils.GetChangelogPath(ssConfig.DBDirectory)
	}
	if err := RecoverStateStore(logger, changelogPath, stateStore); err != nil {
		return nil, err
	}

	// Start the pruning manager for DB
	pruningManager := pruning.NewPruningManager(logger, stateStore, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds))
	pruningManager.Start()
	return stateStore, nil
}

// RecoverStateStore will be called during initialization to recover the state from rlog
func RecoverStateStore(logger logger.Logger, changelogPath string, stateStore types.StateStore) error {
	ssLatestVersion := stateStore.GetLatestVersion()
	logger.Info(fmt.Sprintf("Recovering from changelog %s with latest SS version %d", changelogPath, ssLatestVersion))
	streamHandler, err := wal.NewChangelogWAL(logger, changelogPath, wal.Config{})
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
	if ssLatestVersion > 0 {
		for curVersion > ssLatestVersion && curOffset > firstOffset {
			curOffset--
			curEntry, errRead := streamHandler.ReadAt(curOffset)
			if errRead != nil {
				return err
			}
			curVersion = curEntry.Version
		}
	} else {
		// Fresh store (or no applied versions) – start from the first offset
		curOffset = firstOffset
	}
	// Replay from the offset where the version is larger than SS store latest version
	targetStartOffset := curOffset
	logger.Info(fmt.Sprintf("Start replaying changelog to recover StateStore from offset %d to %d", targetStartOffset, lastOffset))
	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			// commit to state store
			if err := stateStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
				return err
			}
			if err := stateStore.SetLatestVersion(entry.Version); err != nil {
				return err
			}
			return nil
		})
	}
	return nil
}
