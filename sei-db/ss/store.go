package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
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
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	initializer, ok := backends[BackendType(ssConfig.Backend)]
	if !ok {
		return nil, fmt.Errorf("unsupported backend: %s", ssConfig.Backend)
	}
	db, err := initializer(homeDir, ssConfig)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func RecoverStateStore(homePath string, logger logger.Logger, stateStore types.StateStore) error {
	ssLatestVersion, err := stateStore.GetLatestVersion()
	if err != nil {
		return err
	}
	if ssLatestVersion <= 0 {
		return nil
	}
	streamHandler, err := changelog.NewStream(
		logger,
		utils.GetChangelogPath(utils.GetMemIavlDBPath(homePath)),
		changelog.Config{},
	)
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
	firstEntry, errRead := streamHandler.ReadAt(firstOffset)
	if errRead != nil {
		return err
	}
	firstVersion := firstEntry.Version
	delta := uint64(firstVersion) - firstOffset
	targetStartOffset := uint64(ssLatestVersion) - delta
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
