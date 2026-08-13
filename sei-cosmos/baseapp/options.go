package baseapp

import (
	"fmt"
	"io"

	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// File for storing in-package BaseApp optional functions,
// for options that need access to non-exported fields of the BaseApp

// SetPruning sets a pruning option on the multistore associated with the app
func SetPruning(opts sdk.PruningOptions) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.cms.SetPruning(opts) }
}

// SetMinGasPrices returns an option that sets the minimum gas prices on the app.
func SetMinGasPrices(gasPricesStr string) func(*BaseApp) {
	gasPrices, err := sdk.ParseDecCoins(gasPricesStr)
	if err != nil {
		panic(fmt.Sprintf("invalid minimum gas prices: %v", err))
	}

	return func(bapp *BaseApp) { bapp.setMinGasPrices(gasPrices) }
}

// SetHaltHeight returns a BaseApp option function that sets the halt block height.
func SetHaltHeight(blockHeight uint64) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.setHaltHeight(blockHeight) }
}

// SetHaltTime returns a BaseApp option function that sets the halt block time.
func SetHaltTime(haltTime uint64) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.setHaltTime(haltTime) }
}

// SetMinRetainBlocks returns a BaseApp option function that sets the minimum
// block retention height value when determining which heights to prune during
// ABCI Commit.
func SetMinRetainBlocks(minRetainBlocks uint64) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.setMinRetainBlocks(minRetainBlocks) }
}

func SetCompactionInterval(compactionInterval uint64) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.setCompactionInterval(compactionInterval) }
}

// SetTrace will turn on or off trace flag
func SetTrace(trace bool) func(*BaseApp) {
	return func(app *BaseApp) { app.setTrace(trace) }
}

// SetIndexEvents provides a BaseApp option function that sets the events to index.
func SetIndexEvents(ie []string) func(*BaseApp) {
	return func(app *BaseApp) { app.setIndexEvents(ie) }
}

// SetIAVLCacheSize provides a BaseApp option function that sets the size of IAVL cache.
func SetIAVLCacheSize(size int) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.cms.SetIAVLCacheSize(size) }
}

// SetIAVLDisableFastNode enables(false)/disables(true) fast node usage from the IAVL store.
func SetIAVLDisableFastNode(disable bool) func(*BaseApp) {
	return func(bapp *BaseApp) { bapp.cms.SetIAVLDisableFastNode(disable) }
}

// SetInterBlockCache provides a BaseApp option function that sets the
// inter-block cache.
func SetInterBlockCache(cache sdk.MultiStorePersistentCache) func(*BaseApp) {
	return func(app *BaseApp) { app.setInterBlockCache(cache) }
}

// SetSnapshotInterval sets the snapshot interval.
func SetSnapshotInterval(interval uint64) func(*BaseApp) {
	return func(app *BaseApp) { app.SetSnapshotInterval(interval) }
}

func SetConcurrencyWorkers(workers int) func(*BaseApp) {
	return func(app *BaseApp) { app.SetConcurrencyWorkers(workers) }
}

func SetOccEnabled(occEnabled bool) func(*BaseApp) {
	return func(app *BaseApp) { app.SetOccEnabled(occEnabled) }
}

// SetSnapshotKeepRecent sets the recent snapshots to keep.
func SetSnapshotKeepRecent(keepRecent uint32) func(*BaseApp) {
	return func(app *BaseApp) { app.SetSnapshotKeepRecent(keepRecent) }
}

// SetSnapshotDirectory sets the snapshot directory.
func SetSnapshotDirectory(dir string) func(*BaseApp) {
	return func(app *BaseApp) { app.SetSnapshotDirectory(dir) }
}

// SetSnapshotStore sets the snapshot store.
func SetSnapshotStore(snapshotStore *snapshots.Store) func(*BaseApp) {
	return func(app *BaseApp) { app.SetSnapshotStore(snapshotStore) }
}

func (app *BaseApp) SetName(name string) {
	if app.sealed {
		panic("SetName() on sealed BaseApp")
	}

	app.name = name
}

// SetParamStore sets a parameter store on the BaseApp.
func (app *BaseApp) SetParamStore(ps ParamStore) {
	if app.sealed {
		panic("SetParamStore() on sealed BaseApp")
	}

	app.paramStore = ps
}

// SetVersion sets the application's version string.
func (app *BaseApp) SetVersion(v string) {
	if app.sealed {
		panic("SetVersion() on sealed BaseApp")
	}
	app.version = v
}

// SetProtocolVersion sets the application's protocol version
func (app *BaseApp) SetProtocolVersion(v uint64) {
	app.appVersion = v
}

func (app *BaseApp) SetDB(db dbm.DB) {
	if app.sealed {
		panic("SetDB() on sealed BaseApp")
	}

	app.db = db
}

func (app *BaseApp) SetCMS(cms store.CommitMultiStore) {
	if app.sealed {
		panic("SetEndBlocker() on sealed BaseApp")
	}

	app.cms = cms
}

func (app *BaseApp) SetInitChainer(initChainer sdk.InitChainer) {
	if app.sealed {
		panic("SetInitChainer() on sealed BaseApp")
	}

	app.initChainer = initChainer
}

func (app *BaseApp) SetBeginBlocker(beginBlocker sdk.BeginBlocker) {
	if app.sealed {
		panic("SetBeginBlocker() on sealed BaseApp")
	}

	app.beginBlocker = beginBlocker
}

func (app *BaseApp) SetMidBlocker(midBlocker sdk.MidBlocker) {
	if app.sealed {
		panic("SetMidBlocker() on sealed BaseApp")
	}

	app.midBlocker = midBlocker
}

func (app *BaseApp) SetEndBlocker(endBlocker sdk.EndBlocker) {
	if app.sealed {
		panic("SetEndBlocker() on sealed BaseApp")
	}

	app.endBlocker = endBlocker
}

func (app *BaseApp) SetPrepareProposalHandler(prepareProposalHandler sdk.PrepareProposalHandler) {
	if app.sealed {
		panic("SetPrepareProposalHandler() on sealed BaseApp")
	}

	app.prepareProposalHandler = prepareProposalHandler
}

func (app *BaseApp) SetPreCommitHandler(preCommitHandler sdk.PreCommitHandler) {
	if app.sealed {
		panic("SetPreCommitHandler() on sealed BaseApp")
	}

	app.preCommitHandler = preCommitHandler
}

func (app *BaseApp) SetCloseHandler(closeHandler sdk.CloseHandler) {
	if app.sealed {
		panic("SetCloseHandler() on sealed BaseApp")
	}

	app.closeHandler = closeHandler
}

func (app *BaseApp) SetProcessProposalHandler(processProposalHandler sdk.ProcessProposalHandler) {
	if app.sealed {
		panic("SetProcessProposalHandler() on sealed BaseApp")
	}

	app.processProposalHandler = processProposalHandler
}

func (app *BaseApp) SetFinalizeBlocker(finalizeBlocker sdk.FinalizeBlocker) {
	if app.sealed {
		panic("SetFinalizeBlocker() on sealed BaseApp")
	}

	app.finalizeBlocker = finalizeBlocker
}

func (app *BaseApp) SetLoadVersionHandler(loadVersionHandler sdk.LoadVersionHandler) {
	if app.sealed {
		panic("SetLoadVersionHandler() on sealed BaseApp")
	}

	app.loadVersionHandler = loadVersionHandler
}

func (app *BaseApp) SetAnteHandler(ah sdk.AnteHandler) {
	if app.sealed {
		panic("SetAnteHandler() on sealed BaseApp")
	}

	app.anteHandler = ah
}

func (app *BaseApp) SetAnteDepGenerator(adg sdk.AnteDepGenerator) {
	if app.sealed {
		panic("SetAnteDepGenerator() on sealed BaseApp")
	}

	app.anteDepGenerator = adg
}

func (app *BaseApp) SetAddrPeerFilter(pf sdk.PeerFilter) {
	if app.sealed {
		panic("SetAddrPeerFilter() on sealed BaseApp")
	}

	app.addrPeerFilter = pf
}

func (app *BaseApp) SetIDPeerFilter(pf sdk.PeerFilter) {
	if app.sealed {
		panic("SetIDPeerFilter() on sealed BaseApp")
	}

	app.idPeerFilter = pf
}

func (app *BaseApp) SetFauxMerkleMode() {
	if app.sealed {
		panic("SetFauxMerkleMode() on sealed BaseApp")
	}

	app.fauxMerkleMode = true
}

// SetCommitMultiStoreTracer sets the store tracer on the BaseApp's underlying
// CommitMultiStore.
func (app *BaseApp) SetCommitMultiStoreTracer(w io.Writer) {
	app.cms.SetTracer(w)
}

// SetStoreLoader allows us to customize the rootMultiStore initialization.
func (app *BaseApp) SetStoreLoader(loader StoreLoader) {
	if app.sealed {
		panic("SetStoreLoader() on sealed BaseApp")
	}

	app.storeLoader = loader
}

// SetRouter allows us to customize the router.
func (app *BaseApp) SetRouter(router sdk.Router) {
	if app.sealed {
		panic("SetRouter() on sealed BaseApp")
	}
	app.router = router
}

// SetSnapshotStore sets the snapshot store.
func (app *BaseApp) SetSnapshotStore(snapshotStore *snapshots.Store) {
	if app.sealed {
		panic("SetSnapshotStore() on sealed BaseApp")
	}
	if snapshotStore == nil {
		app.snapshotManager = nil
		return
	}
	app.snapshotManager = snapshots.NewManager(snapshotStore, app.cms, app.logger)
}

// SetSnapshotInterval sets the snapshot interval.
func (app *BaseApp) SetSnapshotInterval(snapshotInterval uint64) {
	if app.sealed {
		panic("SetSnapshotInterval() on sealed BaseApp")
	}
	app.snapshotInterval = snapshotInterval
}

func (app *BaseApp) SetConcurrencyWorkers(workers int) {
	if app.sealed {
		panic("SetConcurrencyWorkers() on sealed BaseApp")
	}
	app.concurrencyWorkers = workers
}

func (app *BaseApp) SetOccEnabled(occEnabled bool) {
	if app.sealed {
		panic("SetOccEnabled() on sealed BaseApp")
	}
	app.occEnabled = occEnabled
}

// SetSnapshotKeepRecent sets the number of recent snapshots to keep.
func (app *BaseApp) SetSnapshotKeepRecent(snapshotKeepRecent uint32) {
	if app.sealed {
		panic("SetSnapshotKeepRecent() on sealed BaseApp")
	}
	app.snapshotKeepRecent = snapshotKeepRecent
}

// SetSnapshotDirectory sets the snapshot directory.
func (app *BaseApp) SetSnapshotDirectory(dir string) {
	if app.sealed {
		panic("SetSnapshotDirectory() on sealed BaseApp")
	}
	app.snapshotDirectory = dir
}

// SetInterfaceRegistry sets the InterfaceRegistry.
func (app *BaseApp) SetInterfaceRegistry(registry types.InterfaceRegistry) {
	app.interfaceRegistry = registry
	app.grpcQueryRouter.SetInterfaceRegistry(registry)
	app.msgServiceRouter.SetInterfaceRegistry(registry)
}

// SetStreamingService is used to set a streaming service into the BaseApp hooks and load the listeners into the multistore
func (app *BaseApp) SetStreamingService(s StreamingService) {
	// add the listeners for each StoreKey
	for key, lis := range s.Listeners() {
		app.cms.AddListeners(key, lis)
	}
	// register the StreamingService within the BaseApp
	// BaseApp will pass BeginBlock, DeliverTx, and EndBlock requests and responses to the streaming services to update their ABCI context
	app.abciListeners = append(app.abciListeners, s)
}

// SetQueryMultiStore set a alternative MultiStore implementation to support online migration fallback read.
func (app *BaseApp) SetQueryMultiStore(ms sdk.CommitMultiStore) {
	app.qms = ms
}

// SetMigrationHeight set the migration height for online migration so that query below this height will still be served from IAVL.
func (app *BaseApp) SetMigrationHeight(height int64) {
	app.migrationHeight = height
}
