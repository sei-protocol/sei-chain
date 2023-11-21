package sc

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/sei-protocol/sei-db/common/utils"
	memiavl "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"github.com/sei-protocol/sei-db/sc/memiavl/store/rootmulti"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	FlagSCEnable           = "state-commit.sc-enable"
	FlagAsyncCommitBuffer  = "state-commit.async-commit-buffer"
	FlagZeroCopy           = "state-commit.zero-copy"
	FlagSnapshotKeepRecent = "state-commit.sc-keep-recent"
	FlagSnapshotInterval   = "state-commit.sc-snapshot-interval"
	FlagCacheSize          = "state-commit.sc-cache-size"
)

// SetupStateCommit will replace the default rootmulti store with memiavl store
func SetupStateCommit(
	logger log.Logger,
	homePath string,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) ([]func(*baseapp.BaseApp), *rootmulti.Store) {
	scEnabled := cast.ToBool(appOpts.Get(FlagSCEnable))
	if !scEnabled {
		return baseAppOptions, nil
	}
	logger.Info("Setting up state commit with memiavl")
	opts := memiavl.Options{
		AsyncCommitBuffer:        cast.ToInt(appOpts.Get(FlagAsyncCommitBuffer)),
		ZeroCopy:                 cast.ToBool(appOpts.Get(FlagZeroCopy)),
		SnapshotKeepRecent:       cast.ToUint32(appOpts.Get(FlagSnapshotKeepRecent)),
		SnapshotInterval:         cast.ToUint32(appOpts.Get(FlagSnapshotInterval)),
		CacheSize:                cast.ToInt(appOpts.Get(FlagCacheSize)),
		SnapshotWriterLimit:      4,
		SdkBackwardCompatible:    true,
		ExportNonSnapshotVersion: false,
	}
	// cms must be overridden before the other options, because they may use the cms,
	// make sure the cms aren't be overridden by the other options later on.
	cms := rootmulti.NewStore(utils.GetMemIavlDBPath(homePath), logger, opts)
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
			// trigger state-sync snapshot creation by memiavl
			opts.TriggerStateSyncExport = func(height int64) {
				logger.Info("Triggering memIAVL state snapshot creation")
				baseApp.SnapshotIfApplicable(uint64(height))
			}
			cms.SetMemIAVLOptions(opts)
			baseApp.SetCMS(cms)
		},
	}, baseAppOptions...)

	return baseAppOptions, cms
}
