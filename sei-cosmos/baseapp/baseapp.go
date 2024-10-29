package baseapp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	"github.com/gogo/protobuf/proto"
	sdbm "github.com/sei-protocol/sei-tm-db/backends"
	"github.com/spf13/cast"
	leveldbutils "github.com/syndtr/goleveldb/leveldb/util"
	abci "github.com/tendermint/tendermint/abci/types"
	tmcfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/codec/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/legacy/legacytx"
)

const (
	runTxModeCheck    runTxMode = iota // Check a transaction
	runTxModeReCheck                   // Recheck a (pending) transaction after a commit
	runTxModeSimulate                  // Simulate a transaction
	runTxModeDeliver                   // Deliver a transaction
)

var modeKeyToString = map[runTxMode]string{
	runTxModeCheck:    "check",
	runTxModeReCheck:  "recheck",
	runTxModeSimulate: "simulate",
	runTxModeDeliver:  "deliver",
}

const (
	// archival related flags
	FlagArchivalVersion                = "archival-version"
	FlagArchivalDBType                 = "archival-db-type"
	FlagArchivalArweaveIndexDBFullPath = "archival-arweave-index-db-full-path"
	FlagArchivalArweaveNodeURL         = "archival-arweave-node-url"

	FlagChainID            = "chain-id"
	FlagConcurrencyWorkers = "concurrency-workers"
	FlagOccEnabled         = "occ-enabled"
)

var (
	_ abci.Application = (*BaseApp)(nil)
)

type (
	// Enum mode for app.runTx
	runTxMode uint8

	// StoreLoader defines a customizable function to control how we load the CommitMultiStore
	// from disk. This is useful for state migration, when loading a datastore written with
	// an older version of the software. In particular, if a module changed the substore key name
	// (or removed a substore) between two versions of the software.
	StoreLoader func(ms sdk.CommitMultiStore) error

	DeliverTxHook func(sdk.Context, sdk.Tx, [32]byte, sdk.DeliverTxHookInput)
)

// BaseApp reflects the ABCI application implementation.
type BaseApp struct { //nolint: maligned
	// initialized on creation
	logger            log.Logger
	name              string // application name from abci.Info
	interfaceRegistry types.InterfaceRegistry
	txDecoder         sdk.TxDecoder // unmarshal []byte into sdk.Tx

	anteDepGenerator       sdk.AnteDepGenerator // ante dep generator for parallelization
	prepareProposalHandler sdk.PrepareProposalHandler
	processProposalHandler sdk.ProcessProposalHandler
	finalizeBlocker        sdk.FinalizeBlocker
	anteHandler            sdk.AnteHandler // ante handler for fee and auth
	loadVersionHandler     sdk.LoadVersionHandler
	preCommitHandler       sdk.PreCommitHandler
	closeHandler           sdk.CloseHandler

	appStore
	baseappVersions
	peerFilters
	snapshotData
	abciData
	moduleRouter

	// volatile states:
	//
	// checkState is set on InitChain and reset on Commit
	// deliverState is set on InitChain and BeginBlock and set to nil on Commit
	checkState           *state // for CheckTx
	deliverState         *state // for DeliverTx
	prepareProposalState *state
	processProposalState *state
	stateToCommit        *state

	// paramStore is used to query for ABCI consensus parameters from an
	// application parameter store.
	paramStore ParamStore

	// The minimum gas prices a validator is willing to accept for processing a
	// transaction. This is mainly used for DoS and spam prevention.
	minGasPrices sdk.DecCoins

	// initialHeight is the initial height at which we start the baseapp
	initialHeight int64

	// flag for sealing options and parameters to a BaseApp
	sealed bool

	// block height at which to halt the chain and gracefully shutdown
	haltHeight uint64

	// minimum block time (in Unix seconds) at which to halt the chain and gracefully shutdown
	haltTime uint64

	// minRetainBlocks defines the minimum block height offset from the current
	// block being committed, such that all blocks past this offset are pruned
	// from Tendermint. It is used as part of the process of determining the
	// ResponseCommit.RetainHeight value during ABCI Commit. A value of 0 indicates
	// that no blocks should be pruned.
	//
	// Note: Tendermint block pruning is dependant on this parameter in conunction
	// with the unbonding (safety threshold) period, state pruning and state sync
	// snapshot parameters to determine the correct minimum value of
	// ResponseCommit.RetainHeight.
	minRetainBlocks uint64

	// recovery handler for app.runTx method
	runTxRecoveryMiddleware recoveryMiddleware

	// trace set will return full stack traces for errors in ABCI Log field
	trace bool

	// indexEvents defines the set of events in the form {eventType}.{attributeKey},
	// which informs Tendermint what to index. If empty, all events will be indexed.
	indexEvents map[string]struct{}

	// abciListeners for hooking into the ABCI message processing of the BaseApp
	// and exposing the requests and responses to external consumers
	abciListeners []ABCIListener

	ChainID string

	votesInfoLock    sync.RWMutex
	commitLock       *sync.Mutex
	checkTxStateLock *sync.RWMutex

	compactionInterval uint64

	TmConfig *tmcfg.Config

	TracingInfo    *tracing.Info
	TracingEnabled bool

	concurrencyWorkers int
	occEnabled         bool

	deliverTxHooks []DeliverTxHook
}

type appStore struct {
	db              dbm.DB               // common DB backend
	cms             sdk.CommitMultiStore // Main (uncached) state
	qms             sdk.CommitMultiStore // Query multistore used for migration only
	migrationHeight int64
	storeLoader     StoreLoader // function to handle store loading, may be overridden with SetStoreLoader()

	// an inter-block write-through cache provided to the context during deliverState
	interBlockCache sdk.MultiStorePersistentCache

	fauxMerkleMode bool // if true, IAVL MountStores uses MountStoresDB for simulation speed.
}

type moduleRouter struct {
	router           sdk.Router        // handle any kind of message
	queryRouter      sdk.QueryRouter   // router for redirecting query calls
	grpcQueryRouter  *GRPCQueryRouter  // router for redirecting gRPC query calls
	msgServiceRouter *MsgServiceRouter // router for redirecting Msg service messages
}

type abciData struct {
	initChainer  sdk.InitChainer  // initialize state with validators and state blob
	beginBlocker sdk.BeginBlocker // logic to run before any txs
	midBlocker   sdk.MidBlocker   // logic to run after all txs, and to determine valset changes
	endBlocker   sdk.EndBlocker   // logic to run after all txs, and to determine valset changes

	// absent validators from begin block
	voteInfos []abci.VoteInfo
}

type baseappVersions struct {
	// application's version string
	version string

	// application's protocol version that increments on every upgrade
	// if BaseApp is passed to the upgrade keeper's NewKeeper method.
	appVersion uint64
}

// should really get handled in some db struct
// which then has a sub-item, persistence fields
type snapshotData struct {
	// manages snapshots, i.e. dumps of app state at certain intervals
	snapshotManager    *snapshots.Manager
	snapshotInterval   uint64 // block interval between state sync snapshots
	snapshotKeepRecent uint32 // recent state sync snapshots to keep
	snapshotDirectory  string //  state sync snapshots directory
}

// NewBaseApp returns a reference to an initialized BaseApp. It accepts a
// variadic number of option functions, which act on the BaseApp to set
// configuration choices.
//
// NOTE: The db is used to store the version number for now.
func NewBaseApp(
	name string, logger log.Logger, db dbm.DB, txDecoder sdk.TxDecoder, tmConfig *tmcfg.Config, appOpts servertypes.AppOptions, options ...func(*BaseApp),
) *BaseApp {
	cms := store.NewCommitMultiStore(db)
	archivalVersion := cast.ToInt64(appOpts.Get(FlagArchivalVersion))
	if archivalVersion > 0 {
		switch cast.ToString(appOpts.Get(FlagArchivalDBType)) {
		case "arweave":
			indexDbPath := cast.ToString(appOpts.Get(FlagArchivalArweaveIndexDBFullPath))
			arweaveNodeUrl := cast.ToString(appOpts.Get(FlagArchivalArweaveNodeURL))
			arweaveDb, err := sdbm.NewArweaveDB(indexDbPath, arweaveNodeUrl)
			if err != nil {
				panic(err)
			}
			cms = store.NewCommitMultiStoreWithArchival(db, arweaveDb, archivalVersion)
		}
	}

	tp := trace.NewNoopTracerProvider()
	otel.SetTracerProvider(trace.NewNoopTracerProvider())
	tr := tp.Tracer("component-main")
	tracingEnabled := cast.ToBool(appOpts.Get(tracing.FlagTracing))
	if tracingEnabled {
		tp, err := tracing.DefaultTracerProvider()
		if err != nil {
			panic(err)
		}
		otel.SetTracerProvider(tp)
		tr = tp.Tracer("component-main")
	}
	app := &BaseApp{
		logger: logger,
		name:   name,
		appStore: appStore{
			db:             db,
			cms:            cms,
			storeLoader:    DefaultStoreLoader,
			fauxMerkleMode: false,
		},
		moduleRouter: moduleRouter{
			router:           NewRouter(),
			queryRouter:      NewQueryRouter(),
			grpcQueryRouter:  NewGRPCQueryRouter(),
			msgServiceRouter: NewMsgServiceRouter(),
		},
		txDecoder:      txDecoder,
		TmConfig:       tmConfig,
		TracingEnabled: tracingEnabled,
		TracingInfo: &tracing.Info{
			Tracer: &tr,
		},
		commitLock:       &sync.Mutex{},
		checkTxStateLock: &sync.RWMutex{},
		deliverTxHooks:   []DeliverTxHook{},
	}

	app.TracingInfo.SetContext(context.Background())

	for _, option := range options {
		option(app)
	}

	if app.interBlockCache != nil {
		app.cms.SetInterBlockCache(app.interBlockCache)
	}

	app.runTxRecoveryMiddleware = newDefaultRecoveryMiddleware()
	app.ChainID = cast.ToString(appOpts.Get(FlagChainID))
	if app.ChainID == "" {
		panic("must pass --chain-id when calling 'seid start' or set in ~/.sei/config/client.toml")
	}
	app.startCompactionRoutine(db)

	// if no option overrode already, initialize to the flags value
	// this avoids forcing every implementation to pass an option, but allows it
	if app.concurrencyWorkers == 0 {
		app.concurrencyWorkers = cast.ToInt(appOpts.Get(FlagConcurrencyWorkers))
	}
	// safely default this to the default value if 0
	if app.concurrencyWorkers == 0 {
		app.concurrencyWorkers = config.DefaultConcurrencyWorkers
	}

	return app
}

// Name returns the name of the BaseApp.
func (app *BaseApp) Name() string {
	return app.name
}

// AppVersion returns the application's protocol version.
func (app *BaseApp) AppVersion() uint64 {
	return app.appVersion
}

// ConcurrencyWorkers returns the number of concurrent workers for the BaseApp.
func (app *BaseApp) ConcurrencyWorkers() int {
	return app.concurrencyWorkers
}

// OccEnabled returns the whether OCC is enabled for the BaseApp.
func (app *BaseApp) OccEnabled() bool {
	return app.occEnabled
}

// Version returns the application's version string.
func (app *BaseApp) Version() string {
	return app.version
}

// Logger returns the logger of the BaseApp.
func (app *BaseApp) Logger() log.Logger {
	return app.logger
}

// Trace returns the boolean value for logging error stack traces.
func (app *BaseApp) Trace() bool {
	return app.trace
}

// MsgServiceRouter returns the MsgServiceRouter of a BaseApp.
func (app *BaseApp) MsgServiceRouter() *MsgServiceRouter { return app.msgServiceRouter }

// MountStores mounts all IAVL or DB stores to the provided keys in the BaseApp
// multistore.
func (app *BaseApp) MountStores(keys ...sdk.StoreKey) {
	for _, key := range keys {
		switch key.(type) {
		case *sdk.KVStoreKey:
			if !app.fauxMerkleMode {
				app.MountStore(key, sdk.StoreTypeIAVL)
			} else {
				// StoreTypeDB doesn't do anything upon commit, and it doesn't
				// retain history, but it's useful for faster simulation.
				app.MountStore(key, sdk.StoreTypeDB)
			}

		case *sdk.TransientStoreKey:
			app.MountStore(key, sdk.StoreTypeTransient)

		default:
			panic("Unrecognized store key type " + reflect.TypeOf(key).Name())
		}
	}
}

// MountKVStores mounts all IAVL or DB stores to the provided keys in the
// BaseApp multistore.
func (app *BaseApp) MountKVStores(keys map[string]*sdk.KVStoreKey) {
	for _, key := range keys {
		if !app.fauxMerkleMode {
			app.MountStore(key, sdk.StoreTypeIAVL)
		} else {
			// StoreTypeDB doesn't do anything upon commit, and it doesn't
			// retain history, but it's useful for faster simulation.
			app.MountStore(key, sdk.StoreTypeDB)
		}
	}
}

// MountTransientStores mounts all transient stores to the provided keys in
// the BaseApp multistore.
func (app *BaseApp) MountTransientStores(keys map[string]*sdk.TransientStoreKey) {
	for _, key := range keys {
		app.MountStore(key, sdk.StoreTypeTransient)
	}
}

// MountMemoryStores mounts all in-memory KVStores with the BaseApp's internal
// commit multi-store.
func (app *BaseApp) MountMemoryStores(keys map[string]*sdk.MemoryStoreKey) {
	for _, memKey := range keys {
		app.MountStore(memKey, sdk.StoreTypeMemory)
	}
}

// MountStore mounts a store to the provided key in the BaseApp multistore,
// using the default DB.
func (app *BaseApp) MountStore(key sdk.StoreKey, typ sdk.StoreType) {
	app.cms.MountStoreWithDB(key, typ, nil)
	if app.qms != nil {
		app.qms.MountStoreWithDB(key, typ, nil)
	}
}

// LoadLatestVersion loads the latest application version. It will panic if
// called more than once on a running BaseApp.
func (app *BaseApp) LoadLatestVersion() error {
	err := app.storeLoader(app.cms)
	if err != nil {
		return fmt.Errorf("failed to load latest version: %w", err)
	}

	if app.qms != nil {
		err = app.storeLoader(app.qms)
		if err != nil {
			return fmt.Errorf("failed to load latest version: %w", err)
		}
	}

	return app.init()
}

// DefaultStoreLoader will be used by default and loads the latest version
func DefaultStoreLoader(ms sdk.CommitMultiStore) error {
	return ms.LoadLatestVersion()
}

// CommitMultiStore returns the root multi-store.
// App constructor can use this to access the `cms`.
// UNSAFE: must not be used during the abci life cycle.
func (app *BaseApp) CommitMultiStore() sdk.CommitMultiStore {
	return app.cms
}

// SnapshotManager returns the snapshot manager.
// application use this to register extra extension snapshotters.
func (app *BaseApp) SnapshotManager() *snapshots.Manager {
	return app.snapshotManager
}

// LoadVersion loads the BaseApp application version. It will panic if called
// more than once on a running baseapp.
func (app *BaseApp) LoadVersion(version int64) error {
	err := app.cms.LoadVersion(version)
	if err != nil {
		return fmt.Errorf("failed to load version %d: %w", version, err)
	}
	return app.init()
}

// LoadVersionWithoutInit loads the BaseApp application version, it doesn't call app.init any more,
// specifically used by export genesis command.
func (app *BaseApp) LoadVersionWithoutInit(version int64) error {
	err := app.cms.LoadVersion(version)
	app.setCheckState(tmproto.Header{})
	return err
}

// LastCommitID returns the last CommitID of the multistore.
func (app *BaseApp) LastCommitID() sdk.CommitID {
	return app.cms.LastCommitID()
}

// LastBlockHeight returns the last committed block height.
func (app *BaseApp) LastBlockHeight() int64 {
	return app.cms.LastCommitID().Version
}

func (app *BaseApp) init() error {
	if app.sealed {
		panic("cannot call initFromMainStore: baseapp already sealed")
	}

	// needed for the export command which inits from store but never calls initchain
	app.setCheckState(tmproto.Header{})
	app.Seal()

	return nil
}

func (app *BaseApp) setMinGasPrices(gasPrices sdk.DecCoins) {
	app.minGasPrices = gasPrices
}

func (app *BaseApp) setHaltHeight(haltHeight uint64) {
	app.haltHeight = haltHeight
}

func (app *BaseApp) setHaltTime(haltTime uint64) {
	app.haltTime = haltTime
}

func (app *BaseApp) setMinRetainBlocks(minRetainBlocks uint64) {
	app.minRetainBlocks = minRetainBlocks
}

func (app *BaseApp) setInterBlockCache(cache sdk.MultiStorePersistentCache) {
	app.interBlockCache = cache
}

func (app *BaseApp) setCompactionInterval(compactionInterval uint64) {
	app.compactionInterval = compactionInterval
}

func (app *BaseApp) setTrace(trace bool) {
	app.trace = trace
}

func (app *BaseApp) setIndexEvents(ie []string) {
	app.indexEvents = make(map[string]struct{})

	for _, e := range ie {
		app.indexEvents[e] = struct{}{}
	}
}

// Router returns the router of the BaseApp.
func (app *BaseApp) Router() sdk.Router {
	if app.sealed {
		// We cannot return a Router when the app is sealed because we can't have
		// any routes modified which would cause unexpected routing behavior.
		panic("Router() on sealed BaseApp")
	}

	return app.router
}

// QueryRouter returns the QueryRouter of a BaseApp.
func (app *BaseApp) QueryRouter() sdk.QueryRouter { return app.queryRouter }

// Seal seals a BaseApp. It prohibits any further modifications to a BaseApp.
func (app *BaseApp) Seal() { app.sealed = true }

// IsSealed returns true if the BaseApp is sealed and false otherwise.
func (app *BaseApp) IsSealed() bool { return app.sealed }

// setCheckState sets the BaseApp's checkState with a branched multi-store
// (i.e. a CacheMultiStore) and a new Context with the same multi-store branch,
// provided header, and minimum gas prices set. It is set on InitChain and reset
// on Commit.
func (app *BaseApp) setCheckState(header tmproto.Header) {
	ms := app.cms.CacheMultiStore()
	ctx := sdk.NewContext(ms, header, true, app.logger).WithMinGasPrices(app.minGasPrices)
	app.checkTxStateLock.Lock()
	defer app.checkTxStateLock.Unlock()
	if app.checkState == nil {
		app.checkState = &state{
			ms:  ms,
			ctx: ctx,
			mtx: &sync.RWMutex{},
		}
		return
	}
	app.checkState.SetMultiStore(ms)
	app.checkState.SetContext(ctx)
}

// setDeliverState sets the BaseApp's deliverState with a branched multi-store
// (i.e. a CacheMultiStore) and a new Context with the same multi-store branch,
// and provided header. It is set on InitChain and BeginBlock and set to nil on
// Commit.
func (app *BaseApp) setDeliverState(header tmproto.Header) {
	ms := app.cms.CacheMultiStore()
	ctx := sdk.NewContext(ms, header, false, app.logger)
	if app.deliverState == nil {
		app.deliverState = &state{
			ms:  ms,
			ctx: ctx,
			mtx: &sync.RWMutex{},
		}
		return
	}
	app.deliverState.SetMultiStore(ms)
	app.deliverState.SetContext(ctx)
}

func (app *BaseApp) setPrepareProposalState(header tmproto.Header) {
	ms := app.cms.CacheMultiStore()
	ctx := sdk.NewContext(ms, header, false, app.logger)
	if app.prepareProposalState == nil {
		app.prepareProposalState = &state{
			ms:  ms,
			ctx: ctx,
			mtx: &sync.RWMutex{},
		}
		return
	}
	app.prepareProposalState.SetMultiStore(ms)
	app.prepareProposalState.SetContext(ctx)
}

func (app *BaseApp) setProcessProposalState(header tmproto.Header) {
	ms := app.cms.CacheMultiStore()
	ctx := sdk.NewContext(ms, header, false, app.logger)
	if app.processProposalState == nil {
		app.processProposalState = &state{
			ms:  ms,
			ctx: ctx,
			mtx: &sync.RWMutex{},
		}
		return
	}
	app.processProposalState.SetMultiStore(ms)
	app.processProposalState.SetContext(ctx)
}

func (app *BaseApp) resetStatesExceptCheckState() {
	app.prepareProposalState = nil
	app.processProposalState = nil
	app.deliverState = nil
	app.stateToCommit = nil
}

func (app *BaseApp) setPrepareProposalHeader(header tmproto.Header) {
	app.prepareProposalState.SetContext(app.prepareProposalState.Context().WithBlockHeader(header))
}

func (app *BaseApp) setProcessProposalHeader(header tmproto.Header) {
	app.processProposalState.SetContext(app.processProposalState.Context().WithBlockHeader(header))
}

func (app *BaseApp) setDeliverStateHeader(header tmproto.Header) {
	app.deliverState.SetContext(app.deliverState.Context().WithBlockHeader(header).WithBlockHeight(header.Height))
}

func (app *BaseApp) preparePrepareProposalState() {
	if app.prepareProposalState.MultiStore().TracingEnabled() {
		app.prepareProposalState.SetMultiStore(app.prepareProposalState.MultiStore().SetTracingContext(nil).(sdk.CacheMultiStore))
	}
}

func (app *BaseApp) prepareProcessProposalState(headerHash []byte) {
	app.processProposalState.SetContext(app.processProposalState.Context().
		WithHeaderHash(headerHash).
		WithConsensusParams(app.GetConsensusParams(app.processProposalState.Context())))

	if app.processProposalState.MultiStore().TracingEnabled() {
		app.processProposalState.SetMultiStore(app.processProposalState.MultiStore().SetTracingContext(nil).(sdk.CacheMultiStore))
	}
}

func (app *BaseApp) prepareDeliverState(headerHash []byte) {
	app.deliverState.SetContext(app.deliverState.Context().
		WithHeaderHash(headerHash).
		WithConsensusParams(app.GetConsensusParams(app.deliverState.Context())))
}

func (app *BaseApp) setVotesInfo(votes []abci.VoteInfo) {
	app.votesInfoLock.Lock()
	defer app.votesInfoLock.Unlock()

	app.voteInfos = votes
}

// GetConsensusParams returns the current consensus parameters from the BaseApp's
// ParamStore. If the BaseApp has no ParamStore defined, nil is returned.
func (app *BaseApp) GetConsensusParams(ctx sdk.Context) *tmproto.ConsensusParams {
	if app.paramStore == nil {
		return nil
	}

	cp := new(tmproto.ConsensusParams)

	if app.paramStore.Has(ctx, ParamStoreKeyBlockParams) {
		var bp tmproto.BlockParams

		app.paramStore.Get(ctx, ParamStoreKeyBlockParams, &bp)
		cp.Block = &bp
	}

	if app.paramStore.Has(ctx, ParamStoreKeyEvidenceParams) {
		var ep tmproto.EvidenceParams

		app.paramStore.Get(ctx, ParamStoreKeyEvidenceParams, &ep)
		cp.Evidence = &ep
	}

	if app.paramStore.Has(ctx, ParamStoreKeyValidatorParams) {
		var vp tmproto.ValidatorParams

		app.paramStore.Get(ctx, ParamStoreKeyValidatorParams, &vp)
		cp.Validator = &vp
	}

	if app.paramStore.Has(ctx, ParamStoreKeyVersionParams) {
		var vp tmproto.VersionParams

		app.paramStore.Get(ctx, ParamStoreKeyVersionParams, &vp)
		cp.Version = &vp
	}

	if app.paramStore.Has(ctx, ParamStoreKeySynchronyParams) {
		var vp tmproto.SynchronyParams

		app.paramStore.Get(ctx, ParamStoreKeySynchronyParams, &vp)
		cp.Synchrony = &vp
	}

	if app.paramStore.Has(ctx, ParamStoreKeyTimeoutParams) {
		var vp tmproto.TimeoutParams

		app.paramStore.Get(ctx, ParamStoreKeyTimeoutParams, &vp)
		cp.Timeout = &vp
	}

	if app.paramStore.Has(ctx, ParamStoreKeyABCIParams) {
		var vp tmproto.ABCIParams

		app.paramStore.Get(ctx, ParamStoreKeyABCIParams, &vp)
		cp.Abci = &vp
	}

	return cp
}

// AddRunTxRecoveryHandler adds custom app.runTx method panic handlers.
func (app *BaseApp) AddRunTxRecoveryHandler(handlers ...RecoveryHandler) {
	for _, h := range handlers {
		app.runTxRecoveryMiddleware = newRecoveryMiddleware(h, app.runTxRecoveryMiddleware)
	}
}

// StoreConsensusParams sets the consensus parameters to the baseapp's param store.
func (app *BaseApp) StoreConsensusParams(ctx sdk.Context, cp *tmproto.ConsensusParams) {
	if app.paramStore == nil {
		panic("cannot store consensus params with no params store set")
	}

	if cp == nil {
		return
	}

	app.paramStore.Set(ctx, ParamStoreKeyBlockParams, cp.Block)
	app.paramStore.Set(ctx, ParamStoreKeyEvidenceParams, cp.Evidence)
	app.paramStore.Set(ctx, ParamStoreKeyValidatorParams, cp.Validator)
	app.paramStore.Set(ctx, ParamStoreKeyVersionParams, cp.Version)
	app.paramStore.Set(ctx, ParamStoreKeySynchronyParams, cp.Synchrony)
	app.paramStore.Set(ctx, ParamStoreKeyTimeoutParams, cp.Timeout)
	app.paramStore.Set(ctx, ParamStoreKeyABCIParams, cp.Abci)
}

func (app *BaseApp) validateHeight(req abci.RequestBeginBlock) error {
	if req.Header.Height < 1 {
		return fmt.Errorf("invalid height: %d", req.Header.Height)
	}

	// expectedHeight holds the expected height to validate.
	var expectedHeight int64
	if app.LastBlockHeight() == 0 && app.initialHeight > 1 {
		// In this case, we're validating the first block of the chain (no
		// previous commit). The height we're expecting is the initial height.
		expectedHeight = app.initialHeight
	} else {
		// This case can means two things:
		// - either there was already a previous commit in the store, in which
		// case we increment the version from there,
		// - or there was no previous commit, and initial version was not set,
		// in which case we start at version 1.
		expectedHeight = app.LastBlockHeight() + 1
	}

	if req.Header.Height != expectedHeight {
		return fmt.Errorf("invalid height: %d; expected: %d", req.Header.Height, expectedHeight)
	}

	return nil
}

// validateBasicTxMsgs executes basic validator calls for messages.
func validateBasicTxMsgs(msgs []sdk.Msg) error {
	if len(msgs) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "must contain at least one message")
	}

	for _, msg := range msgs {
		err := msg.ValidateBasic()
		if err != nil {
			return err
		}
	}

	return nil
}

// Returns the applications's deliverState if app is in runTxModeDeliver,
// otherwise it returns the application's checkstate.
func (app *BaseApp) getState(mode runTxMode) *state {
	if mode == runTxModeDeliver {
		return app.deliverState
	}

	return app.checkState
}

// retrieve the context for the tx w/ txBytes and other memoized values.
func (app *BaseApp) getContextForTx(mode runTxMode, txBytes []byte) sdk.Context {
	app.votesInfoLock.RLock()
	defer app.votesInfoLock.RUnlock()
	ctx := app.getState(mode).Context().
		WithTxBytes(txBytes).
		WithVoteInfos(app.voteInfos)

	ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))

	if mode == runTxModeReCheck {
		ctx = ctx.WithIsReCheckTx(true)
	}

	if mode == runTxModeSimulate {
		ctx, _ = ctx.CacheContext()
	}

	return ctx
}

// cacheTxContext returns a new context based off of the provided context with
// a branched multi-store.
func (app *BaseApp) cacheTxContext(ctx sdk.Context, checksum [32]byte) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	// TODO: https://github.com/cosmos/cosmos-sdk/issues/2824
	msCache := ms.CacheMultiStore()
	if msCache.TracingEnabled() {
		msCache = msCache.SetTracingContext(
			sdk.TraceContext(
				map[string]interface{}{
					"txHash": fmt.Sprintf("%X", checksum),
				},
			),
		).(sdk.CacheMultiStore)
	}

	return ctx.WithMultiStore(msCache), msCache
}

// runTx processes a transaction within a given execution mode, encoded transaction
// bytes, and the decoded transaction itself. All state transitions occur through
// a cached Context depending on the mode provided. State only gets persisted
// if all messages get executed successfully and the execution mode is DeliverTx.
// Note, gas execution info is always returned. A reference to a Result is
// returned if the tx does not run out of gas and if all the messages are valid
// and execute successfully. An error is returned otherwise.
func (app *BaseApp) runTx(ctx sdk.Context, mode runTxMode, tx sdk.Tx, checksum [32]byte) (
	gInfo sdk.GasInfo,
	result *sdk.Result,
	anteEvents []abci.Event,
	priority int64,
	pendingTxChecker abci.PendingTxChecker,
	expireHandler abci.ExpireTxHandler,
	txCtx sdk.Context,
	err error,
) {
	defer telemetry.MeasureThroughputSinceWithLabels(
		telemetry.TxCount,
		[]metrics.Label{
			telemetry.NewLabel("mode", modeKeyToString[mode]),
		},
		time.Now(),
	)

	// Reset events after each checkTx or simulateTx or recheckTx
	// DeliverTx is garbage collected after FinalizeBlocker
	if mode != runTxModeDeliver {
		defer ctx.MultiStore().ResetEvents()
	}

	// Wait for signals to complete before starting the transaction. This is needed before any of the
	// resources are acceessed by the ante handlers and message handlers.
	defer acltypes.SendAllSignalsForTx(ctx.TxCompletionChannels())
	acltypes.WaitForAllSignalsForTx(ctx.TxBlockingChannels())
	if app.TracingEnabled {
		// check for existing parent tracer, and if applicable, use it
		spanCtx, span := app.TracingInfo.StartWithContext("RunTx", ctx.TraceSpanContext())
		defer span.End()
		ctx = ctx.WithTraceSpanContext(spanCtx)
		span.SetAttributes(attribute.String("txHash", fmt.Sprintf("%X", checksum)))
	}

	// NOTE: GasWanted should be returned by the AnteHandler. GasUsed is
	// determined by the GasMeter. We need access to the context to get the gas
	// meter so we initialize upfront.
	var gasWanted uint64

	ms := ctx.MultiStore()

	defer func() {
		if r := recover(); r != nil {
			acltypes.SendAllSignalsForTx(ctx.TxCompletionChannels())
			recoveryMW := newOutOfGasRecoveryMiddleware(gasWanted, ctx, app.runTxRecoveryMiddleware)
			recoveryMW = newOCCAbortRecoveryMiddleware(recoveryMW) // TODO: do we have to wrap with occ enabled check?
			err, result = processRecovery(r, recoveryMW), nil
			if mode != runTxModeDeliver {
				ctx.MultiStore().ResetEvents()
			}
		}
		gInfo = sdk.GasInfo{GasWanted: gasWanted, GasUsed: ctx.GasMeter().GasConsumed()}
	}()

	if tx == nil {
		return sdk.GasInfo{}, nil, nil, 0, nil, nil, ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "tx decode error")
	}

	msgs := tx.GetMsgs()

	if err := validateBasicTxMsgs(msgs); err != nil {
		return sdk.GasInfo{}, nil, nil, 0, nil, nil, ctx, err
	}

	if app.anteHandler != nil {
		var anteSpan trace.Span
		if app.TracingEnabled {
			// trace AnteHandler
			_, anteSpan = app.TracingInfo.StartWithContext("AnteHandler", ctx.TraceSpanContext())
			defer anteSpan.End()
		}
		var (
			anteCtx sdk.Context
			msCache sdk.CacheMultiStore
		)
		// Branch context before AnteHandler call in case it aborts.
		// This is required for both CheckTx and DeliverTx.
		// Ref: https://github.com/cosmos/cosmos-sdk/issues/2772
		//
		// NOTE: Alternatively, we could require that AnteHandler ensures that
		// writes do not happen if aborted/failed.  This may have some
		// performance benefits, but it'll be more difficult to get right.
		anteCtx, msCache = app.cacheTxContext(ctx, checksum)
		anteCtx = anteCtx.WithEventManager(sdk.NewEventManager())
		newCtx, err := app.anteHandler(anteCtx, tx, mode == runTxModeSimulate)

		if !newCtx.IsZero() {
			// At this point, newCtx.MultiStore() is a store branch, or something else
			// replaced by the AnteHandler. We want the original multistore.
			//
			// Also, in the case of the tx aborting, we need to track gas consumed via
			// the instantiated gas meter in the AnteHandler, so we update the context
			// prior to returning.
			//
			// This also replaces the GasMeter in the context where GasUsed was initalized 0
			// and updated with gas consumed in the ante handler runs
			// The GasMeter is a pointer and its passed to the RunMsg and tracks the consumed
			// gas there too.
			ctx = newCtx.WithMultiStore(ms)
		}
		defer func() {
			if newCtx.DeliverTxCallback() != nil {
				newCtx.DeliverTxCallback()(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)))
			}
		}()

		events := ctx.EventManager().Events()

		// GasMeter expected to be set in AnteHandler
		gasWanted = ctx.GasMeter().Limit()
		if err != nil {
			return gInfo, nil, nil, 0, nil, nil, ctx, err
		}

		// Dont need to validate in checkTx mode
		if ctx.MsgValidator() != nil && mode == runTxModeDeliver {
			storeAccessOpEvents := msCache.GetEvents()
			accessOps := ctx.TxMsgAccessOps()[acltypes.ANTE_MSG_INDEX]

			// TODO: (occ) This is an example of where we do our current validation. Note that this validation operates on the declared dependencies for a TX / antehandler + the utilized dependencies, whereas the validation
			missingAccessOps := ctx.MsgValidator().ValidateAccessOperations(accessOps, storeAccessOpEvents)
			if len(missingAccessOps) != 0 {
				for op := range missingAccessOps {
					ctx.Logger().Info((fmt.Sprintf("Antehandler Missing Access Operation:%s ", op.String())))
					op.EmitValidationFailMetrics()
				}
				errMessage := fmt.Sprintf("Invalid Concurrent Execution antehandler missing %d access operations", len(missingAccessOps))
				return gInfo, nil, nil, 0, nil, nil, ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidConcurrencyExecution, errMessage)
			}
		}

		priority = ctx.Priority()
		pendingTxChecker = ctx.PendingTxChecker()
		expireHandler = ctx.ExpireTxHandler()
		msCache.Write()
		anteEvents = events.ToABCIEvents()
		if app.TracingEnabled {
			anteSpan.End()
		}
	}

	// Create a new Context based off of the existing Context with a MultiStore branch
	// in case message processing fails. At this point, the MultiStore
	// is a branch of a branch.
	runMsgCtx, msCache := app.cacheTxContext(ctx, checksum)

	// Attempt to execute all messages and only update state if all messages pass
	// and we're in DeliverTx. Note, runMsgs will never return a reference to a
	// Result if any single message fails or does not have a registered Handler.
	result, err = app.runMsgs(runMsgCtx, msgs, mode)

	if err == nil && mode == runTxModeDeliver {
		msCache.Write()
	}
	// we do this since we will only be looking at result in DeliverTx
	if result != nil && len(anteEvents) > 0 {
		// append the events in the order of occurrence
		result.Events = append(anteEvents, result.Events...)
	}
	if ctx.CheckTxCallback() != nil {
		ctx.CheckTxCallback()(ctx, err)
	}
	// only apply hooks if no error
	if err == nil && (!ctx.IsEVM() || result.EvmError == "") {
		var evmTxInfo *abci.EvmTxInfo
		if ctx.IsEVM() {
			evmTxInfo = &abci.EvmTxInfo{
				SenderAddress: ctx.EVMSenderAddress(),
				Nonce:         ctx.EVMNonce(),
				TxHash:        ctx.EVMTxHash(),
				VmError:       result.EvmError,
			}
		}
		var events []abci.Event = []abci.Event{}
		if result != nil {
			events = sdk.MarkEventsToIndex(result.Events, app.indexEvents)
		}
		for _, hook := range app.deliverTxHooks {
			hook(ctx, tx, checksum, sdk.DeliverTxHookInput{
				EvmTxInfo: evmTxInfo,
				Events:    events,
			})
		}
	}
	return gInfo, result, anteEvents, priority, pendingTxChecker, expireHandler, ctx, err
}

// runMsgs iterates through a list of messages and executes them with the provided
// Context and execution mode. Messages will only be executed during simulation
// and DeliverTx. An error is returned if any single message fails or if a
// Handler does not exist for a given message route. Otherwise, a reference to a
// Result is returned. The caller must not commit state if an error is returned.
func (app *BaseApp) runMsgs(ctx sdk.Context, msgs []sdk.Msg, mode runTxMode) (*sdk.Result, error) {

	defer telemetry.MeasureThroughputSinceWithLabels(
		telemetry.MessageCount,
		[]metrics.Label{
			telemetry.NewLabel("mode", modeKeyToString[mode]),
		},
		time.Now(),
	)

	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
			panic(err)
		}
	}()
	if app.TracingEnabled {
		spanCtx, span := app.TracingInfo.StartWithContext("RunMsgs", ctx.TraceSpanContext())
		defer span.End()
		ctx = ctx.WithTraceSpanContext(spanCtx)
	}
	msgLogs := make(sdk.ABCIMessageLogs, 0, len(msgs))
	events := sdk.EmptyEvents()
	txMsgData := &sdk.TxMsgData{
		Data: make([]*sdk.MsgData, 0, len(msgs)),
	}
	var evmError string

	// NOTE: GasWanted is determined by the AnteHandler and GasUsed by the GasMeter.
	for i, msg := range msgs {
		// skip actual execution for (Re)CheckTx mode
		if mode == runTxModeCheck || mode == runTxModeReCheck {
			break
		}
		var (
			msgResult    *sdk.Result
			eventMsgName string // name to use as value in event `message.action`
			err          error
		)

		msgCtx, msgMsCache := app.cacheTxContext(ctx, [32]byte{})
		msgCtx = msgCtx.WithMessageIndex(i)

		startTime := time.Now()
		if handler := app.msgServiceRouter.Handler(msg); handler != nil {
			// ADR 031 request type routing
			msgResult, err = handler(msgCtx, msg)
			eventMsgName = sdk.MsgTypeURL(msg)
			metrics.MeasureSinceWithLabels(
				[]string{"sei", "cosmos", "run", "msg", "latency"},
				startTime,
				[]metrics.Label{{Name: "type", Value: eventMsgName}},
			)
		} else if legacyMsg, ok := msg.(legacytx.LegacyMsg); ok {
			// legacy sdk.Msg routing
			// Assuming that the app developer has migrated all their Msgs to
			// proto messages and has registered all `Msg services`, then this
			// path should never be called, because all those Msgs should be
			// registered within the `msgServiceRouter` already.
			msgRoute := legacyMsg.Route()
			eventMsgName = legacyMsg.Type()
			handler := app.router.Route(msgCtx, msgRoute)
			if handler == nil {
				return nil, sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "unrecognized message route: %s; message index: %d", msgRoute, i)
			}
			msgResult, err = handler(msgCtx, msg)
			metrics.MeasureSinceWithLabels(
				[]string{"cosmos", "run", "msg", "latency"},
				startTime,
				[]metrics.Label{{Name: "type", Value: eventMsgName}},
			)
		} else {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "can't route message %+v", msg)
		}

		if err != nil {
			return nil, sdkerrors.Wrapf(err, "failed to execute message; message index: %d", i)
		}

		msgEvents := sdk.Events{
			sdk.NewEvent(sdk.EventTypeMessage, sdk.NewAttribute(sdk.AttributeKeyAction, eventMsgName)),
		}
		msgEvents = msgEvents.AppendEvents(msgResult.GetEvents())

		// append message events, data and logs
		//
		// Note: Each message result's data must be length-prefixed in order to
		// separate each result.
		events = events.AppendEvents(msgEvents)

		txMsgData.Data = append(txMsgData.Data, &sdk.MsgData{MsgType: sdk.MsgTypeURL(msg), Data: msgResult.Data})
		msgLogs = append(msgLogs, sdk.NewABCIMessageLog(uint32(i), msgResult.Log, msgEvents))

		msgMsCache.Write()

		if msgResult.EvmError != "" {
			evmError = msgResult.EvmError
		}

		if ctx.MsgValidator() == nil {
			continue
		}
		storeAccessOpEvents := msgMsCache.GetEvents()
		accessOps := ctx.TxMsgAccessOps()[i]
		missingAccessOps := ctx.MsgValidator().ValidateAccessOperations(accessOps, storeAccessOpEvents)
		// TODO: (occ) This is where we are currently validating our per message dependencies,
		// whereas validation will be done holistically based on the mvkv for OCC approach
		if len(missingAccessOps) != 0 {
			for op := range missingAccessOps {
				ctx.Logger().Info((fmt.Sprintf("eventMsgName=%s Missing Access Operation:%s ", eventMsgName, op.String())))
				op.EmitValidationFailMetrics()
			}
			errMessage := fmt.Sprintf("Invalid Concurrent Execution messageIndex=%d, missing %d access operations", i, len(missingAccessOps))
			// we need to bubble up the events for inspection
			return &sdk.Result{
				Log:    strings.TrimSpace(msgLogs.String()),
				Events: events.ToABCIEvents(),
			}, sdkerrors.Wrap(sdkerrors.ErrInvalidConcurrencyExecution, errMessage)
		}
	}

	data, err := proto.Marshal(txMsgData)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to marshal tx data")
	}

	return &sdk.Result{
		Data:     data,
		Log:      strings.TrimSpace(msgLogs.String()),
		Events:   events.ToABCIEvents(),
		EvmError: evmError,
	}, nil
}

func (app *BaseApp) GetAnteDepGenerator() sdk.AnteDepGenerator {
	return app.anteDepGenerator
}

func (app *BaseApp) startCompactionRoutine(db dbm.DB) {
	if app.compactionInterval == 0 {
		return
	}
	go func() {
		if goleveldb, ok := db.(*dbm.GoLevelDB); ok {
			for {
				time.Sleep(time.Duration(app.compactionInterval) * time.Second)
				if err := goleveldb.DB().CompactRange(leveldbutils.Range{Start: nil, Limit: nil}); err != nil {
					app.Logger().Error(fmt.Sprintf("error compacting DB: %s", err))
				}
			}
		} else {
			app.Logger().Info("exit compaction routine because underlying DB does not support compaction")
		}
	}()
}

func (app *BaseApp) Close() error {
	// we do not want to close when a commit is ongoing since commit writes to stores
	// and metadata in a non-atomic way
	app.commitLock.Lock()
	defer app.commitLock.Unlock()
	if err := app.appStore.db.Close(); err != nil {
		return err
	}
	// close the underline database for storeV2
	if err := app.cms.Close(); err != nil {
		return err
	}
	if err := app.snapshotManager.Close(); err != nil {
		return err
	}
	if app.closeHandler == nil {
		return nil
	}
	return app.closeHandler()
}

func (app *BaseApp) ReloadDB() error {
	if err := app.db.Close(); err != nil {
		return err
	}
	db, err := sdk.NewLevelDB("application", app.TmConfig.DBDir())
	if err != nil {
		return err
	}
	app.db = db
	app.cms = store.NewCommitMultiStore(db)
	if app.snapshotManager != nil {
		app.snapshotManager.SetMultiStore(app.cms)
	}
	return nil
}

func (app *BaseApp) GetCheckCtx() sdk.Context {
	app.checkTxStateLock.RLock()
	defer app.checkTxStateLock.RUnlock()
	return app.checkState.ctx
}

func (app *BaseApp) RegisterDeliverTxHook(hook DeliverTxHook) {
	app.deliverTxHooks = append(app.deliverTxHooks, hook)
}
