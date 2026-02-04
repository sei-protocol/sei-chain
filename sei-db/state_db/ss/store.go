package ss

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

// PrunableStateStore wraps a StateStore with pruning lifecycle management.
// When Close() is called, it first stops the pruning goroutine, then closes the underlying store.
type PrunableStateStore struct {
	types.StateStore
	pruningManager *pruning.Manager
	closeOnce      sync.Once
	closeErr       error
}

// Close stops the pruning goroutine and then closes the underlying state store.
// This ensures no pruning operations are in progress when the store is closed.
// Safe to call multiple times (idempotent).
func (p *PrunableStateStore) Close() error {
	p.closeOnce.Do(func() {
		// First, stop the pruning goroutine and wait for it to exit
		if p.pruningManager != nil {
			p.pruningManager.Stop()
		}
		// Then close the underlying store
		p.closeErr = p.StateStore.Close()
	})
	return p.closeErr
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
	changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(homeDir, ssConfig.Backend))
	if ssConfig.DBDirectory != "" {
		changelogPath = utils.GetChangelogPath(ssConfig.DBDirectory)
	}
	if err := RecoverStateStore(logger, changelogPath, stateStore); err != nil {
		return nil, err
	}

	// Create pruning manager
	pruningManager := pruning.NewPruningManager(logger, stateStore, int64(ssConfig.KeepRecent), int64(ssConfig.PruneIntervalSeconds))
	pruningManager.Start()

	// Return wrapped store with pruning lifecycle management
	return &PrunableStateStore{
		StateStore:     stateStore,
		pruningManager: pruningManager,
	}, nil
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
