package memiavl

import (
	"path/filepath"

	"github.com/cosmos/cosmos-sdk/baseapp"
	memiavlopts "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"github.com/sei-protocol/sei-db/sc/memiavl/store/rootmulti"
	"github.com/tendermint/tendermint/libs/log"
)

// SetupMemIAVL insert the memiavl setter in front of baseapp options, so that
// the default rootmulti store is replaced by memiavl store,
func SetupMemIAVL(
	logger log.Logger,
	homePath string,
	opts memiavlopts.Options,
	baseAppOptions []func(*baseapp.BaseApp),
) []func(*baseapp.BaseApp) {
	logger.Info("Setting up state commit with memiavl")
	// cms must be overridden before the other options, because they may use the cms,
	// make sure the cms aren't be overridden by the other options later on.
	baseAppOptions = append([]func(*baseapp.BaseApp){
		func(baseApp *baseapp.BaseApp) {
			cms := rootmulti.NewStore(filepath.Join(homePath, "data", "memiavl.db"), logger, opts)
			// trigger state-sync snapshot creation by memiavl
			opts.TriggerStateSyncExport = func(height int64) {
				logger.Info("Triggering memIAVL state snapshot creation")
				baseApp.SnapshotIfApplicable(uint64(height))
			}
			cms.SetMemIAVLOptions(opts)
			baseApp.SetCMS(cms)
		},
	}, baseAppOptions...)

	return baseAppOptions
}
