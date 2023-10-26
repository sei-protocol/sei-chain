package store

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	memiavlopts "github.com/sei-protocol/sei-db/sc/memiavl/db"
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
	if cast.ToBool(appOpts.Get(FlagSCEnable)) {
		logger.Info("SeiDB is enabled")
		opts := memiavlopts.Options{
			AsyncCommitBuffer:        cast.ToInt(appOpts.Get(FlagAsyncCommitBuffer)),
			ZeroCopy:                 cast.ToBool(appOpts.Get(FlagZeroCopy)),
			SnapshotKeepRecent:       cast.ToUint32(appOpts.Get(FlagSnapshotKeepRecent)),
			SnapshotInterval:         cast.ToUint32(appOpts.Get(FlagSnapshotInterval)),
			CacheSize:                cast.ToInt(appOpts.Get(FlagCacheSize)),
			SnapshotWriterLimit:      cast.ToInt(appOpts.Get(FlagSnapshotWriterLimit)),
			SdkBackwardCompatible:    true,
			ExportNonSnapshotVersion: false,
		}
		baseAppOptions = append(memiavl.SetupMemIAVL(logger, homePath, opts, baseAppOptions), baseAppOptions...)
		if cast.ToBool(appOpts.Get(FlagSSEnable)) {
			backendOpt := appOpts.Get(FlagSSBackend)
			logger.Info(fmt.Sprintf("Setting up state store, detected to use DB backend: %s", backendOpt))
		}
	}
	return baseAppOptions
}
