package node

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // nolint: gosec // securely exposed on separate, optional port
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/sdk/trace"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventlog"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	mempoolreactor "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/reactor"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pex"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"

	_ "github.com/grafana/pyroscope-go/godeltaprof/http/pprof"

	_ "github.com/lib/pq" // provide the psql db driver
)

// nodeImpl is the highest level interface to a full Tendermint node.
// It includes all configuration information and running services.
type nodeImpl struct {
	service.BaseService

	// config
	config          *config.Config
	genesisDoc      *types.GenesisDoc   // initial validator set
	privValidator   types.PrivValidator // local node's validator key
	shouldHandshake bool                // set during makeNode

	// network
	router           *p2p.Router
	ServiceRestartCh chan []string
	nodeInfo         types.NodeInfo
	nodeKey          types.NodeKey // our node privkey

	// services
	eventSinks     []indexer.EventSink
	initialState   sm.State
	stateStore     sm.Store
	blockStore     *store.BlockStore // store the blockchain to disk
	mempool        *mempool.TxMempool
	evPool         *evidence.Pool
	indexerService *indexer.Service
	services       []service.Service
	rpcListeners   []net.Listener // rpc servers
	shutdownOps    closer
	rpcEnv         *rpccore.Environment
	prometheusSrv  *http.Server
}

// makeNode returns a new, ready to go, Tendermint Node.
func makeNode(
	ctx context.Context,
	cfg *config.Config,
	restartEvent func(),
	filePrivval *privval.FilePV,
	nodeKey types.NodeKey,
	app abci.Application,
	genesisDocProvider genesisDocProvider,
	dbProvider config.DBProvider,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetrics *NodeMetrics,
) (service.Service, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	closers := []closer{convertCancelCloser(cancel)}
	app = proxy.New(app, nodeMetrics.proxy)

	blockStore, stateDB, dbCloser, err := initDBs(cfg, dbProvider)
	if err != nil {
		return nil, combineCloseError(err, dbCloser)
	}
	closers = append(closers, dbCloser)

	stateStore := sm.NewStore(stateDB)

	genDoc, err := genesisDocProvider()
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}

	if err = genDoc.ValidateAndComplete(); err != nil {
		return nil, combineCloseError(fmt.Errorf("error in genesis doc: %w", err), makeCloser(closers))
	}

	state, err := LoadStateFromDBOrGenesisDocProvider(stateStore, genDoc)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}

	eventBus := eventbus.NewDefault()

	var eventLog *eventlog.Log
	if w := cfg.RPC.EventLogWindowSize; w > 0 {
		var err error
		eventLog, err = eventlog.New(eventlog.LogSettings{
			WindowSize: w,
			MaxItems:   cfg.RPC.EventLogMaxItems,
			Metrics:    nodeMetrics.eventlog,
		})
		if err != nil {
			return nil, combineCloseError(fmt.Errorf("initializing event log: %w", err), makeCloser(closers))
		}
	}
	eventSinks, err := sink.EventSinksFromConfig(cfg, dbProvider, genDoc.ChainID)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	indexerService := indexer.NewService(indexer.ServiceArgs{
		Sinks:    eventSinks,
		EventBus: eventBus,
		Metrics:  nodeMetrics.indexer,
	})

	privValidator, err := createPrivval(ctx, cfg, genDoc, filePrivval)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}

	pubKey := utils.None[crypto.PubKey]()
	if cfg.Mode == config.ModeValidator {
		key, err := privValidator.GetPubKey(ctx)
		if err != nil {
			return nil, combineCloseError(fmt.Errorf("can't get pubkey: %w", err),
				makeCloser(closers))
		}
		pubKey = utils.Some(key)
	}
	// TODO construct node here:
	node := &nodeImpl{
		config:        cfg,
		genesisDoc:    genDoc,
		privValidator: privValidator,

		nodeKey: nodeKey,

		eventSinks:     eventSinks,
		indexerService: indexerService,
		services:       []service.Service{eventBus},

		initialState: state,
		stateStore:   stateStore,
		blockStore:   blockStore,

		rpcEnv: &rpccore.Environment{
			App: app,

			StateStore: stateStore,
			BlockStore: blockStore,
			GenDoc:     genDoc,
			EventSinks: eventSinks,
			EventBus:   eventBus,
			EventLog:   eventLog,
			Config:     *cfg.RPC,
		},
	}

	// Autobahn requires a local validator key; remote signers are not supported.
	if cfg.AutobahnConfigFile != "" && cfg.PrivValidator.ListenAddr != "" {
		return nil, combineCloseError(
			fmt.Errorf("autobahn does not support remote validator signers (priv-validator.laddr is set)"),
			makeCloser(closers))
	}
	gigaEnabled := cfg.AutobahnConfigFile != ""
	mp := mempool.NewTxMempool(cfg.Mempool.ToMempoolConfig(), app, nodeMetrics.mempool, sm.TxConstraintsFetcherFromStore(stateStore))
	router, peerCloser, err := createRouter(
		nodeMetrics.p2p,
		node.NodeInfo,
		nodeKey,
		utils.Some(atypes.SecretKeyFromED25519(filePrivval.Key.PrivKey)),
		cfg,
		utils.Some(mp),
		genDoc,
		dbProvider,
	)
	closers = append(closers, peerCloser)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create router: %w", err),
			makeCloser(closers))
	}
	node.router = router
	node.mempool = mp
	node.rpcEnv.Router = router
	node.shutdownOps = makeCloser(closers)

	// Mempool gossiping is not compatible with Giga,
	// so we disable the mempool reactor.
	if !gigaEnabled {
		mpReactor, err := mempoolreactor.NewReactor(cfg.Mempool, mp, router)
		if err != nil {
			return nil, fmt.Errorf("mempoolreactor.NewReactor(): %w", err)
		}
		mpReactor.MarkReadyToStart()
		node.services = append(node.services, mpReactor)
	}

	evReactor, evPool, edbCloser, err := createEvidenceReactor(cfg, dbProvider,
		stateStore, blockStore, node.router, nodeMetrics.evidence, eventBus)
	closers = append(closers, edbCloser)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	node.services = append(node.services, evReactor)
	node.rpcEnv.EvidencePool = evPool
	node.evPool = evPool

	node.rpcEnv.Mempool = mp

	// make block executor for consensus and blockchain reactors to execute blocks
	blockExec := sm.NewBlockExecutor(
		stateStore,
		app,
		mp,
		evPool,
		blockStore,
		eventBus,
		nodeMetrics.state,
	)

	// Determine whether we should attempt state sync.
	stateSync := cfg.StateSync.Enable && !onlyValidatorIsUs(state, pubKey)
	if stateSync && state.LastBlockHeight > 0 {
		logger.Info("Found local state with non-zero height, skipping state sync")
		stateSync = false
	}

	// Determine whether we should do block sync. This must happen after the handshake, since the
	// app may modify the validator set, specifying ourself as the only validator.
	blockSync := !onlyValidatorIsUs(state, pubKey)
	if gigaEnabled {
		// Autobahn does not use CometBFT block sync — blocks arrive into the
		// data WAL via the giga p2p layer instead.
		blockSync = false
		// The state.LastBlockHeight > 0 guard above doesn't fire for giga:
		// autobahn never writes to CometBFT's state store, so it stays at 0
		// even after a successful run. Ask the app directly — if its CMS
		// holds committed state, we have a local WAL to restart from and
		// don't need state sync. State sync remains available for a joiner
		// or disk-wiped node where the app's CMS is empty; runExecute's
		// last>0 path then resumes from the state-synced height and peers
		// stream subsequent blocks via the giga service.
		if stateSync {
			info, err := app.Info(ctx, &abci.RequestInfo{})
			if err != nil {
				return nil, combineCloseError(fmt.Errorf("app.Info: %w", err), makeCloser(closers))
			}
			if info.LastBlockHeight > 0 {
				logger.Info("giga: found local app state, skipping state sync",
					"height", info.LastBlockHeight)
				stateSync = false
			}
		}
	}
	waitSync := stateSync || blockSync

	csState, err := consensus.NewState(
		cfg.Consensus,
		stateStore,
		blockExec,
		blockStore,
		mp,
		evPool,
		eventBus,
		tracerProviderOptions,
		consensus.StateMetrics(nodeMetrics.consensus),
		consensus.SkipStateStoreBootstrap,
	)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	node.rpcEnv.ConsensusState = csState

	var csReactor *consensus.Reactor
	if !gigaEnabled {
		csReactor, err = consensus.NewReactor(
			csState,
			node.router,
			eventBus,
			waitSync,
			nodeMetrics.consensus,
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf("consensus.NewReactor(): %w", err)
		}

		node.services = append(node.services, csReactor)
		node.rpcEnv.ConsensusReactor = csReactor
	}

	// Create the blockchain reactor. Note, we do not start block sync if we're
	// doing a state sync first.
	bcReactor, err := blocksync.NewReactor(
		stateStore,
		blockExec,
		blockStore,
		csReactor,
		node.router,
		blockSync && !stateSync,
		nodeMetrics.consensus,
		eventBus,
		restartEvent,
		cfg.SelfRemediation,
	)
	if err != nil {
		return nil, fmt.Errorf("blocksync.NewReactor(): %w", err)
	}
	node.services = append(node.services, bcReactor)
	node.rpcEnv.BlockSyncReactor = bcReactor

	// Make ConsensusReactor. Don't enable fully if doing a state sync and/or block sync first.
	// FIXME We need to update metrics here, since other reactors don't have access to them.
	if stateSync {
		nodeMetrics.consensus.StateSyncing.Set(1)
	} else if blockSync {
		nodeMetrics.consensus.BlockSyncing.Set(1)
	}

	if cfg.P2P.PexReactor {
		pxReactor, err := pex.NewReactor(
			node.router,
			pex.DefaultSendInterval,
		)
		if err != nil {
			return nil, fmt.Errorf("pex.NewReactor(): %w", err)
		}
		node.services = append(node.services, pxReactor)
	}

	postSyncHook := func(ctx context.Context, state sm.State) error {
		if gigaEnabled {
			// In giga mode there's no CometBFT block sync or consensus reactor
			// to hand control to — csReactor is nil and bcReactor is inert.
			// The giga router's runExecute picks up from app.Info().LastBlockHeight
			// on its own, so no further action is required here.
			return nil
		}
		csReactor.SetStateSyncingMetrics(0)

		// TODO: Some form of orchestrator is needed here between the state
		// advancing reactors to be able to control which one of the three
		// is running
		// FIXME Very ugly to have these metrics bleed through here.
		csReactor.SetBlockSyncingMetrics(1)
		if err := bcReactor.SwitchToBlockSync(ctx, state); err != nil {
			logger.Error("failed to switch to block sync", "err", err)
			return err
		}

		return nil
	}
	// Set up state sync reactor, and schedule a sync if requested.
	// FIXME The way we do phased startups (e.g. replay -> block sync -> consensus) is very messy,
	// we should clean this whole thing up. See:
	// https://github.com/tendermint/tendermint/issues/4644
	// The CometBFT handshaker reconciles the block store and state store with the app
	// by replaying blocks and calling InitChain at genesis. Autobahn (giga) maintains
	// its own data WAL and does not update the CometBFT block/state stores, so on
	// restart the handshaker would observe storeHeight=0 < appHeight=N and fail with
	// ErrAppBlockHeightTooHigh. We skip the handshaker in giga mode; instead the
	// giga router's runExecute owns InitChain on fresh start (appHeight==0) and
	// relies on the app's committed CMS to rebuild deliverState on restart.
	node.shouldHandshake = !stateSync && !gigaEnabled
	// In giga mode the state sync reactor is only wired when we actually
	// intend to sync (joining node / disk-wiped validator). A plain giga
	// restart skips it (stateSync above was cleared by the app.Info() check).
	if !gigaEnabled || stateSync {
		// In giga mode, inject the autobahn state provider factory so the
		// reactor uses it instead of the RPC/P2P selection. Nil for
		// non-giga callers preserves existing behaviour.
		var stateProviderFactory func(ctx context.Context) (statesync.StateProvider, error)
		if gigaEnabled {
			gd := genDoc
			stateProviderFactory = func(_ context.Context) (statesync.StateProvider, error) {
				return statesync.NewGigaStateProvider(gd), nil
			}
		}
		ssReactor, err := statesync.NewReactor(
			genDoc.ChainID,
			genDoc.InitialHeight,
			*cfg.StateSync,
			app,
			node.router,
			stateStore,
			blockStore,
			cfg.StateSync.TempDir,
			nodeMetrics.statesync,
			eventBus,
			// the post-sync operation
			postSyncHook,
			stateSync,
			restartEvent,
			cfg.SelfRemediation,
			stateProviderFactory,
		)
		if err != nil {
			return nil, fmt.Errorf("statesync.NewReactor(): %w", err)
		}
		node.services = append(node.services, ssReactor)
	}

	if cfg.Mode == config.ModeValidator {
		if privValidator != nil {
			csState.SetPrivValidator(ctx, utils.Some(privValidator))
		}
	}
	node.rpcEnv.PubKey = pubKey

	node.BaseService = *service.NewBaseService("Node", node)

	return node, nil
}

// OnStart starts the Node. It implements service.Service.
func (n *nodeImpl) OnStart(ctx context.Context) error {
	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped or crashed after it saved the block
	// but before it indexed the txs)
	if err := n.rpcEnv.EventBus.Start(ctx); err != nil {
		return err
	}

	if err := n.indexerService.Start(ctx); err != nil {
		return err
	}

	// state sync will cover initialization the chain. Also calling InitChain isn't safe
	// when there is state sync as InitChain itself doesn't commit application state which
	// would get mixed up with application state writes by state sync.
	if n.shouldHandshake {
		// Create the handshaker, which calls RequestInfo, sets the AppVersion on the state,
		// and replays any blocks as necessary to sync tendermint with the app.
		if err := consensus.NewHandshaker(
			n.stateStore, n.initialState, n.blockStore, n.rpcEnv.EventBus, n.genesisDoc,
		).Handshake(ctx, n.rpcEnv.App); err != nil {
			return err
		}
	}

	// Reload the state. It will have the Version.Consensus.App set by the
	// Handshake, and may have other modifications as well (ie. depending on
	// what happened during block replay).
	state, err := n.stateStore.Load()
	if err != nil {
		return fmt.Errorf("cannot load state: %w", err)
	}

	logNodeStartupInfo(state, n.rpcEnv.PubKey, n.config.Mode)

	// TODO: Fetch and provide real options and do proper p2p bootstrapping.
	// TODO: Use a persistent peer database.
	n.nodeInfo, err = makeNodeInfo(n.config, n.nodeKey, n.eventSinks, n.genesisDoc, state.Version.Consensus)
	if err != nil {
		return err
	}
	// Start Internal Services

	if n.config.RPC.PprofListenAddress != "" {
		signal := make(chan struct{})
		srv := &http.Server{
			Addr:              n.config.RPC.PprofListenAddress,
			Handler:           nil,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			select {
			case <-ctx.Done():
				sctx, scancel := context.WithTimeout(context.Background(), time.Second)
				defer scancel()
				_ = srv.Shutdown(sctx)
			case <-signal:
			}
		}()

		go func() {
			logger.Info("Starting pprof server", "laddr", n.config.RPC.PprofListenAddress)

			if err := srv.ListenAndServe(); err != nil {
				logger.Error("pprof server error", "err", err)
				close(signal)
			}
		}()
	}

	now := tmtime.Now()
	genTime := n.genesisDoc.GenesisTime
	if genTime.After(now) {
		logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)

		timer := time.NewTimer(genTime.Sub(now))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	state, err = n.stateStore.Load()
	if err != nil {
		return err
	}
	if err := n.evPool.Start(state); err != nil {
		return err
	}

	if n.config.Instrumentation.Prometheus && n.config.Instrumentation.PrometheusListenAddr != "" {
		n.prometheusSrv = n.startPrometheusServer(ctx, n.config.Instrumentation.PrometheusListenAddr)
	}

	// Start the transport.
	if err := n.router.Start(ctx); err != nil {
		return err
	}
	n.rpcEnv.IsListening = true
	n.SpawnCritical("mempool", n.mempool.Run)

	for _, reactor := range n.services {
		if err := reactor.Start(ctx); err != nil {
			return fmt.Errorf("problem starting service '%T': %w ", reactor, err)
		}
	}

	n.rpcEnv.NodeInfo = n.nodeInfo
	// Start the RPC server before the P2P server
	// so we can eg. receive txs for the first block
	if n.config.RPC.ListenAddress != "" {
		var err error
		n.rpcListeners, err = n.rpcEnv.StartService(ctx, n.config)
		if err != nil {
			return err
		}
	}

	return nil
}

// OnStop stops the Node. It implements service.Service.
func (n *nodeImpl) OnStop() {
	logger.Info("Stopping Node")
	// stop the listeners / external services first
	for _, l := range n.rpcListeners {
		logger.Info("Closing rpc listener", "listener", l.Addr())
		if err := l.Close(); err != nil {
			logger.Error("error closing listener", "listener", l.Addr(), "err", err)
		}
	}

	for _, es := range n.eventSinks {
		if err := es.Stop(); err != nil {
			logger.Error("failed to stop event sink", "err", err)
		}
	}

	for _, reactor := range n.services {
		reactor.Stop()
	}

	n.router.Stop()
	n.router.Wait()
	n.rpcEnv.IsListening = false

	if pvsc, ok := n.privValidator.(service.Service); ok {
		pvsc.Stop()
		pvsc.Wait()
	}

	if n.prometheusSrv != nil {
		if err := n.prometheusSrv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			logger.Error("Prometheus HTTP server Shutdown", "err", err)
		}

	}
	if err := n.shutdownOps(); err != nil {
		if strings.TrimSpace(err.Error()) != "" {
			logger.Error("problem shutting down additional services", "err", err)
		}
	}
	if n.blockStore != nil {
		if err := n.blockStore.Close(); err != nil {
			logger.Error("problem closing blockstore", "err", err)
		}
	}
	if n.stateStore != nil {
		if err := n.stateStore.Close(); err != nil {
			logger.Error("problem closing statestore", "err", err)
		}
	}
}

// startPrometheusServer starts a Prometheus HTTP server, listening for metrics
// collectors on addr.
func (n *nodeImpl) startPrometheusServer(ctx context.Context, addr string) *http.Server {
	srv := &http.Server{
		Addr: addr,
		Handler: promhttp.InstrumentMetricHandler(
			prometheus.DefaultRegisterer, promhttp.HandlerFor(
				prometheus.DefaultGatherer,
				promhttp.HandlerOpts{MaxRequestsInFlight: n.config.Instrumentation.MaxOpenConnections},
			),
		),
		ReadHeaderTimeout: 10 * time.Second, //nolint:gosec // G112: mitigate slowloris attacks
	}

	signal := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			sctx, scancel := context.WithTimeout(context.Background(), time.Second)
			defer scancel()
			_ = srv.Shutdown(sctx)
		case <-signal:
		}
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			logger.Error("Prometheus HTTP server ListenAndServe", "err", err)
			close(signal)
		}
	}()

	return srv
}

func (n *nodeImpl) NodeInfo() *types.NodeInfo {
	return &n.nodeInfo
}

// EventBus returns the Node's EventBus.
func (n *nodeImpl) EventBus() *eventbus.EventBus {
	return n.rpcEnv.EventBus
}

// GenesisDoc returns the Node's GenesisDoc.
func (n *nodeImpl) GenesisDoc() *types.GenesisDoc {
	return n.genesisDoc
}

// RPCEnvironment makes sure RPC has all the objects it needs to operate.
func (n *nodeImpl) RPCEnvironment() *rpccore.Environment {
	return n.rpcEnv
}

//------------------------------------------------------------------------------

// genesisDocProvider returns a GenesisDoc.
// It allows the GenesisDoc to be pulled from sources other than the
// filesystem, for instance from a distributed key-value store cluster.
type genesisDocProvider func() (*types.GenesisDoc, error)

// defaultGenesisDocProviderFunc returns a GenesisDocProvider that loads
// the GenesisDoc from the config.GenesisFile() on the filesystem.
func defaultGenesisDocProviderFunc(cfg *config.Config) genesisDocProvider {
	return func() (*types.GenesisDoc, error) {
		return types.GenesisDocFromFile(cfg.GenesisFile())
	}
}

type NodeMetrics struct {
	consensus *consensus.Metrics
	eventlog  *eventlog.Metrics
	indexer   *indexer.Metrics
	mempool   *mempool.Metrics
	p2p       *p2p.Metrics
	proxy     *proxy.Metrics
	state     *sm.Metrics
	statesync *statesync.Metrics
	evidence  *evidence.Metrics
}

// metricsProvider returns consensus, p2p, mempool, state, statesync Metrics.
type metricsProvider func(chainID string) *NodeMetrics

func NoOpMetricsProvider() *NodeMetrics {
	return &NodeMetrics{
		consensus: consensus.NopMetrics(),
		indexer:   indexer.NopMetrics(),
		mempool:   mempool.NopMetrics(),
		p2p:       p2p.NopMetrics(),
		proxy:     proxy.NopMetrics(),
		state:     sm.NopMetrics(),
		statesync: statesync.NopMetrics(),
		evidence:  evidence.NopMetrics(),
	}
}

// defaultMetricsProvider returns Metrics build using Prometheus client library
// if Prometheus is enabled. Otherwise, it returns no-op Metrics.
func DefaultMetricsProvider(cfg *config.InstrumentationConfig) metricsProvider {
	return func(chainID string) *NodeMetrics {
		if cfg.Prometheus {
			return &NodeMetrics{
				consensus: consensus.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				eventlog:  eventlog.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				indexer:   indexer.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				mempool:   mempool.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				p2p:       p2p.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				proxy:     proxy.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				state:     sm.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				statesync: statesync.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
				evidence:  evidence.PrometheusMetrics(cfg.Namespace, "chain_id", chainID),
			}
		}
		return NoOpMetricsProvider()
	}
}

//------------------------------------------------------------------------------

// LoadStateFromDBOrGenesisDocProvider attempts to load the state from the
// database, or creates one using the given genesisDocProvider. On success this also
// returns the genesis doc loaded through the given provider.
func LoadStateFromDBOrGenesisDocProvider(stateStore sm.Store, genDoc *types.GenesisDoc) (sm.State, error) {

	// 1. Attempt to load state form the database
	state, err := stateStore.Load()
	if err != nil {
		return sm.State{}, err
	}

	if state.IsEmpty() {
		// 2. If it's not there, derive it from the genesis doc
		state, err = sm.MakeGenesisState(genDoc)
		if err != nil {
			return sm.State{}, err
		}

		// 3. save the gensis document to the state store so
		// its fetchable by other callers.
		if err := stateStore.Save(state); err != nil {
			return sm.State{}, err
		}
	}

	return state, nil
}
