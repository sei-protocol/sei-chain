package seidb

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	memiavldb "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/sei-protocol/sei-db/stream/service"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	FlagSCEnable            = "state-commit.enable"
	FlagAsyncCommitBuffer   = "state-commit.async-commit-buffer"
	FlagZeroCopy            = "state-commit.zero-copy"
	FlagSnapshotKeepRecent  = "state-commit.snapshot-keep-recent"
	FlagSnapshotInterval    = "state-commit.snapshot-interval"
	FlagCacheSize           = "state-commit.cache-size"
	FlagSnapshotWriterLimit = "state-commit.snapshot-writer-limit"
	FlagSSEnable            = "state-store.enable"
	FlagSSBackend           = "state-store.backend"
	FlagSSAsyncFlush        = "state-store.async-flush"
)

func SetupSeiDB(
	logger log.Logger,
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) []func(*baseapp.BaseApp) {
	SCEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	SSEnabled := cast.ToBool(appOpts.Get(FlagSSEnable))
	AsyncFlush := cast.ToBool(appOpts.Get(FlagSSAsyncFlush))
	opts := memiavldb.Options{
		AsyncCommitBuffer:        cast.ToInt(appOpts.Get(FlagAsyncCommitBuffer)),
		ZeroCopy:                 cast.ToBool(appOpts.Get(FlagZeroCopy)),
		SnapshotKeepRecent:       cast.ToUint32(appOpts.Get(FlagSnapshotKeepRecent)),
		SnapshotInterval:         cast.ToUint32(appOpts.Get(FlagSnapshotInterval)),
		CacheSize:                cast.ToInt(appOpts.Get(FlagCacheSize)),
		SnapshotWriterLimit:      cast.ToInt(appOpts.Get(FlagSnapshotWriterLimit)),
		SdkBackwardCompatible:    true,
		ExportNonSnapshotVersion: false,
	}
	if !SCEnabled {
		return baseAppOptions
	}
	if SSEnabled {
		logger.Info(fmt.Sprintf("State Store is enabled for storing historical data"))
		stateStore := ss.SetupStateStore(homePath, appOpts)
		if AsyncFlush {
			_, err := startSubscriberService(logger, homePath, stateStore)
			if err != nil {
				panic(err)
			}
		} else {
			opts.CommitInterceptor = func(version int64, changesets []*proto.NamedChangeSet) error {
				return commitToStateStore(stateStore, version, changesets)
			}
		}
	}
	logger.Info("State Commit is enabled, setting up to memIAVL")
	baseAppOptions = append(memiavl.SetupMemIAVL(logger, homePath, opts, baseAppOptions), baseAppOptions...)
	return baseAppOptions
}

// commitToStateStore is a helper function to commit changesets to state store
func commitToStateStore(stateStore ss.StateStore, version int64, changesets []*proto.NamedChangeSet) error {
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

func startSubscriberService(logger logger.Logger, homePath string, stateStore ss.StateStore) (*service.Subscriber, error) {
	subscriber := service.NewSubscriber(logger, homePath, func(index uint64, entry proto.ChangelogEntry) error {
		return commitToStateStore(stateStore, entry.Version, entry.Changesets)
	})
	initialVersion, err := memiavldb.GetInitialVersion(homePath)
	if err != nil {
		return nil, err
	}
	lastPersistedVersion, err := stateStore.GetLatestVersion()
	if err != nil {
		return nil, err
	}
	err = subscriber.Initialize(initialVersion, lastPersistedVersion)
	if err != nil {
		return nil, err
	}
	lastestOffset, err := subscriber.GetLatestOffset()
	if err != nil {
		return nil, err
	}
	subscriber.Start(lastestOffset)
	return subscriber, nil
}
