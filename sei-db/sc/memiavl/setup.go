package memiavl

import (
	"path/filepath"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	memiavl "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"github.com/sei-protocol/sei-db/sc/memiavl/store/rootmulti"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	FlagMemIAVL                  = "memiavl.enable"
	FlagAsyncCommitBuffer        = "memiavl.async-commit-buffer"
	FlagZeroCopy                 = "memiavl.zero-copy"
	FlagSnapshotKeepRecent       = "memiavl.snapshot-keep-recent"
	FlagSnapshotInterval         = "memiavl.snapshot-interval"
	FlagCacheSize                = "memiavl.cache-size"
	FlagSnapshotWriterLimit      = "memiavl.snapshot-writer-limit"
	FlagSDKBackwardCompatible    = "memiavl.sdk-backward-compatible"
	FlagExportNonSnapshotVersion = "memiavl.export-non-snapshot-version"
)

// SetupMemIAVL insert the memiavl setter in front of baseapp options, so that
// the default rootmulti store is replaced by memiavl store,
func SetupMemIAVL(logger log.Logger, homePath string, appOpts servertypes.AppOptions, baseAppOptions []func(*baseapp.BaseApp)) []func(*baseapp.BaseApp) {
	if cast.ToBool(appOpts.Get(FlagMemIAVL)) {
		opts := memiavl.Options{
			AsyncCommitBuffer:        cast.ToInt(appOpts.Get(FlagAsyncCommitBuffer)),
			ZeroCopy:                 cast.ToBool(appOpts.Get(FlagZeroCopy)),
			SnapshotKeepRecent:       cast.ToUint32(appOpts.Get(FlagSnapshotKeepRecent)),
			SnapshotInterval:         cast.ToUint32(appOpts.Get(FlagSnapshotInterval)),
			CacheSize:                cast.ToInt(appOpts.Get(FlagCacheSize)),
			SnapshotWriterLimit:      cast.ToInt(appOpts.Get(FlagSnapshotWriterLimit)),
			SdkBackwardCompatible:    cast.ToBool(appOpts.Get(FlagSDKBackwardCompatible)),
			ExportNonSnapshotVersion: cast.ToBool(appOpts.Get(FlagExportNonSnapshotVersion)),
		}

		// cms must be overridden before the other options, because they may use the cms,
		// make sure the cms aren't be overridden by the other options later on.
		baseAppOptions = append([]func(*baseapp.BaseApp){
			func(baseApp *baseapp.BaseApp) {
				cms := rootmulti.NewStore(filepath.Join(homePath, "data", "memiavl.db"), logger, opts)
				cms.SetMemIAVLOptions(opts)
				// trigger state-sync snapshot creation by memiavl
				opts.TriggerStateSyncExport = func(height int64) {
					logger.Info("Triggering memIAVL state snapshot creation")
					baseApp.SnapshotIfApplicable(uint64(height))
				}
				baseApp.SetCMS(cms)
			},
		}, baseAppOptions...)
	}

	return baseAppOptions
}
