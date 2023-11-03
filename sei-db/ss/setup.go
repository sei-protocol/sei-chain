package ss

import (
	"fmt"
	"path/filepath"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/sc/memiavl/store/rootmulti"
	"github.com/sei-protocol/sei-db/ss/store"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"github.com/spf13/cast"
)

const (
	FlagSSEnable            = "state-store.enable"
	FlagSSBackend           = "state-store.backend"
	FlagSSAsyncWriterBuffer = "state-store.async-write-buffer"
)

func SetupStateStore(
	logger logger.Logger,
	homePath string,
	cms storetypes.CommitMultiStore,
	stateCommit *rootmulti.Store,
	appOpts servertypes.AppOptions,
	keys map[string]*storetypes.KVStoreKey,
	tkeys map[string]*storetypes.TransientStoreKey,
	memKeys map[string]*storetypes.MemoryStoreKey,
) (storetypes.QueryMultiStore, error) {
	ssEnabled := cast.ToBool(appOpts.Get(FlagSSEnable))
	if !ssEnabled {
		return nil, nil
	}
	ssBuffer := cast.ToInt(appOpts.Get(FlagSSAsyncWriterBuffer))
	ssBackend := cast.ToString(appOpts.Get(FlagSSBackend))
	logger.Info(fmt.Sprintf("State Store is enabled with %s backend", ssBackend))

	ss, err := createStateStore(homePath, BackendType(ssBackend))
	if err != nil {
		return nil, err
	}

	// default to exposing all
	exposeStoreKeys := make(map[string]storetypes.StoreKey, len(keys))
	for k, storeKey := range keys {
		exposeStoreKeys[k] = storeKey
	}

	// Setup Commit Subscriber
	subscriber := changelog.NewSubscriber(ssBuffer, func(entry proto.ChangelogEntry) error {
		return commitToStateStore(ss, entry.Version, entry.Changesets)
	})
	subscriber.Start()
	stateCommit.SetCommitSubscriber(subscriber)
	// replay to recover the SS and catch up till latest changelog
	err = recoverStateStore(logger, ss, homePath)
	if err != nil {
		return nil, err
	}
	logger.Info("Finished replaying changelog for SS")

	// Setup QueryMultiStore
	qms := store.NewMultiStore(cms, ss, exposeStoreKeys)
	qms.MountTransientStores(tkeys)
	qms.MountMemoryStores(memKeys)
	return qms, nil
}

func createStateStore(homePath string, backendType BackendType) (types.StateStore, error) {
	dbDirectory := filepath.Join(homePath, "data", string(backendType))
	database, err := NewStateStoreDB(dbDirectory, backendType)
	if err != nil {
		return nil, err
	}
	return database, nil
}

// commitToStateStore is a helper function to commit changesets to state store
func commitToStateStore(stateStore types.StateStore, version int64, changesets []*proto.NamedChangeSet) error {
	for _, cs := range changesets {
		err := stateStore.ApplyChangeset(version, cs)
		if err != nil {
			return err
		}
		err = stateStore.SetLatestVersion(version)
		if err != nil {
			return err
		}
	}
	return nil
}

// recoverStateStore is a helper function to recover the missing entries in SS during initialization
func recoverStateStore(logger logger.Logger, stateStore types.StateStore, homePath string) error {
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
	targetStartOffset := uint64(ssLatestVersion) + delta
	logger.Info(fmt.Sprintf("Start replaying changelog for SS from offset %d to %d", targetStartOffset, lastOffset))
	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			return commitToStateStore(stateStore, entry.Version, entry.Changesets)
		})
	}
	return nil
}
