package seidb

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	memiavldb "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/sei-protocol/sei-db/stream/changelog"
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
	FlagSSAsyncWriterBuffer = "state-store.async-write-buffer"
)

func SetupSeiDB(
	logger log.Logger,
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) []func(*baseapp.BaseApp) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	ssEnabled := cast.ToBool(appOpts.Get(FlagSSEnable))
	ssBuffer := cast.ToInt(appOpts.Get(FlagSSAsyncWriterBuffer))
	ssBackend := cast.ToString(appOpts.Get(FlagSSBackend))
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
	if !scEnabled {
		return baseAppOptions
	}
	if ssEnabled {
		logger.Info("State Store is enabled for storing historical data")
		stateStore := ss.SetupStateStore(homePath, ss.BackendType(ssBackend))
		subscriber := changelog.NewSubscriber(ssBuffer, func(entry proto.ChangelogEntry) error {
			return commitToStateStore(stateStore, entry.Version, entry.Changesets)
		})
		subscriber.Start()
		opts.CommitSubscriber = subscriber
		// Do replays to recover the missing entries in SS
		err := recoverStateStore(stateStore, homePath)
		if err != nil {
			panic(err)
		}
	}
	logger.Info("State Commit is enabled, setting up to memIAVL")
	baseAppOptions = append(memiavl.SetupMemIAVL(logger, homePath, opts, baseAppOptions), baseAppOptions...)
	return baseAppOptions
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

func recoverStateStore(stateStore types.StateStore, homePath string) error {
	ssLatestVersion, err := stateStore.GetLatestVersion()
	if err != nil {
		return err
	}
	if ssLatestVersion <= 0 {
		return nil
	}
	streamHandler, err := changelog.NewStream(
		log.NewNopLogger(),
		utils.GetChangelogPath(utils.GetMemIavlDBPath(homePath)),
		changelog.Config{},
	)
	if err != nil {
		return err
	}
	firstOffset, err := streamHandler.FirstOffset()
	if firstOffset <= 0 || err != nil {
		return err
	}
	lastOffset, err := streamHandler.LastOffset()
	if lastOffset <= 0 || err != nil {
		return err
	}
	firstEntry, err := streamHandler.ReadAt(firstOffset)
	firstVersion := firstEntry.Version
	delta := uint64(firstVersion) - firstOffset
	targetStartOffset := uint64(ssLatestVersion) + delta
	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			return commitToStateStore(stateStore, entry.Version, entry.Changesets)
		})
	}
	return nil
}
