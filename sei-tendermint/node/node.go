package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/sdk/trace"

	abciclient "github.com/tendermint/tendermint/abci/client"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/blocksync"
	"github.com/tendermint/tendermint/internal/consensus"
	"github.com/tendermint/tendermint/internal/dbsync"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/eventlog"
	"github.com/tendermint/tendermint/internal/evidence"
	"github.com/tendermint/tendermint/internal/mempool"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/p2p/pex"
	"github.com/tendermint/tendermint/internal/proxy"
	rpccore "github.com/tendermint/tendermint/internal/rpc/core"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/state/indexer"
	"github.com/tendermint/tendermint/internal/state/indexer/sink"
	"github.com/tendermint/tendermint/internal/statesync"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	tmtime "github.com/tendermint/tendermint/libs/time"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"

	_ "net/http/pprof" // nolint: gosec // securely exposed on separate, optional port

	_ "github.com/grafana/pyroscope-go/godeltaprof/http/pprof"

	_ "github.com/lib/pq" // provide the psql db driver
)

// nodeImpl is the highest level interface to a full Tendermint node.
// It includes all configuration information and running services.
type nodeImpl struct {
	service.BaseService
	logger log.Logger

	// config
	config          *config.Config
	genesisDoc      *types.GenesisDoc   // initial validator set
	privValidator   types.PrivValidator // local node's validator key
	shouldHandshake bool                // set during makeNode

	// network
	peerManager      *p2p.PeerManager
	router           *p2p.Router
	routerRestartCh  chan struct{} // Used to signal a restart the node on the application level
	ServiceRestartCh chan []string
	nodeInfo         types.NodeInfo
	nodeKey          types.NodeKey // our node privkey

	// services
	eventSinks     []indexer.EventSink
	initialState   sm.State
	stateStore     sm.Store
	blockStore     *store.BlockStore // store the blockchain to disk
	evPool         *evidence.Pool
	indexerService *indexer.Service
	services       []service.Service
	rpcListeners   []net.Listener // rpc servers
	shutdownOps    closer
	rpcEnv         *rpccore.Environment
	prometheusSrv  *http.Server
}

// newDefaultNode returns a Tendermint node with default settings for the
// PrivValidator, ClientCreator, GenesisDoc, and DBProvider.
// It implements NodeProvider.
func newDefaultNode(
	ctx context.Context,
	cfg *config.Config,
	logger log.Logger,
	restartCh chan struct{},
) (service.Service, error) {
	nodeKey, err := types.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load or gen node key %s: %w", cfg.NodeKeyFile(), err)
	}

	appClient, _, err := proxy.ClientFactory(logger, cfg.ProxyApp, cfg.ABCI, cfg.DBDir())
	if err != nil {
		return nil, err
	}

	if cfg.Mode == config.ModeSeed {
		return makeSeedNode(
			ctx,
			logger,
			cfg,
			restartCh,
			config.DefaultDBProvider,
			nodeKey,
			defaultGenesisDocProviderFunc(cfg),
			appClient,
			DefaultMetricsProvider(cfg.Instrumentation)(cfg.ChainID()),
		)
	}
	pval, err := makeDefaultPrivval(cfg)
	if err != nil {
		return nil, err
	}

	return makeNode(
		ctx,
		cfg,
		restartCh,
		pval,
		nodeKey,
		appClient,
		defaultGenesisDocProviderFunc(cfg),
		config.DefaultDBProvider,
		logger,
		[]trace.TracerProviderOption{},
		DefaultMetricsProvider(cfg.Instrumentation)(cfg.ChainID()),
	)
}

// makeNode returns a new, ready to go, Tendermint Node.
func makeNode(
	ctx context.Context,
	cfg *config.Config,
	restartCh chan struct{},
	filePrivval *privval.FilePV,
	nodeKey types.NodeKey,
	client abciclient.Client,
	genesisDocProvider genesisDocProvider,
	dbProvider config.DBProvider,
	logger log.Logger,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetrics *NodeMetrics,
) (service.Service, error) {

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	closers := []closer{convertCancelCloser(cancel)}

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

	proxyApp := proxy.New(client, logger.With("module", "proxy"), nodeMetrics.proxy)
	eventBus := eventbus.NewDefault(logger.With("module", "events"))

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
		Logger:   logger.With("module", "txindex"),
		Metrics:  nodeMetrics.indexer,
	})

	privValidator, err := createPrivval(ctx, logger, cfg, genDoc, filePrivval)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}

	var pubKey crypto.PubKey
	pubKey, err = privValidator.GetPubKey(ctx)
	if err != nil {
		return nil, combineCloseError(fmt.Errorf("can't get pubkey: %w", err),
			makeCloser(closers))
	}

	if cfg.Mode == config.ModeValidator {
		if pubKey == nil {
			return nil, combineCloseError(
				errors.New("could not retrieve public key from private validator"),
				makeCloser(closers))
		}
	}

	peerManager, peerCloser, err := createPeerManager(logger, cfg, dbProvider, nodeKey.ID, nodeMetrics.p2p)
	closers = append(closers, peerCloser)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create peer manager: %w", err),
			makeCloser(closers))
	}

	// TODO construct node here:
	node := &nodeImpl{
		config:        cfg,
		logger:        logger,
		genesisDoc:    genDoc,
		privValidator: privValidator,

		peerManager: peerManager,
		nodeKey:     nodeKey,

		eventSinks:     eventSinks,
		indexerService: indexerService,
		services:       []service.Service{eventBus},

		initialState: state,
		stateStore:   stateStore,
		blockStore:   blockStore,

		shutdownOps: makeCloser(closers),

		rpcEnv: &rpccore.Environment{
			ProxyApp: proxyApp,

			StateStore: stateStore,
			BlockStore: blockStore,

			PeerManager: peerManager,

			GenDoc:     genDoc,
			EventSinks: eventSinks,
			EventBus:   eventBus,
			EventLog:   eventLog,
			Logger:     logger.With("module", "rpc"),
			Config:     *cfg.RPC,
		},
	}

	node.router, err = createRouter(logger, nodeMetrics.p2p, node.NodeInfo, nodeKey, peerManager, cfg, proxyApp)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create router: %w", err),
			makeCloser(closers))
	}

	evReactor, evPool, edbCloser, err := createEvidenceReactor(logger, cfg, dbProvider,
		stateStore, blockStore, peerManager.Subscribe, nodeMetrics.evidence, eventBus)
	node.router.AddChDescToBeAdded(evidence.GetChannelDescriptor(), evReactor.SetChannel)
	closers = append(closers, edbCloser)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	node.services = append(node.services, evReactor)
	node.rpcEnv.EvidencePool = evPool
	node.evPool = evPool

	info, err := client.Info(ctx, &abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	shoulddbsync := cfg.DBSync.Enable && info.LastBlockHeight == 0

	mpReactor, mp := createMempoolReactor(logger, cfg, proxyApp, stateStore, nodeMetrics.mempool,
		peerManager.Subscribe, peerManager)
	node.router.AddChDescToBeAdded(mempool.GetChannelDescriptor(cfg.Mempool), mpReactor.SetChannel)
	if !shoulddbsync {
		mpReactor.MarkReadyToStart()
	}
	node.rpcEnv.Mempool = mp
	node.services = append(node.services, mpReactor)

	// make block executor for consensus and blockchain reactors to execute blocks
	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger.With("module", "state"),
		proxyApp,
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

	if stateSync && shoulddbsync {
		panic("statesync and dbsync cannot be turned on at the same time")
	}

	// Determine whether we should do block sync. This must happen after the handshake, since the
	// app may modify the validator set, specifying ourself as the only validator.
	blockSync := !onlyValidatorIsUs(state, pubKey)
	waitSync := stateSync || blockSync || shoulddbsync

	csState, err := consensus.NewState(logger.With("module", "consensus"),
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

	csReactor := consensus.NewReactor(
		logger,
		csState,
		peerManager.Subscribe,
		eventBus,
		waitSync,
		nodeMetrics.consensus,
		cfg,
	)

	node.router.AddChDescToBeAdded(consensus.GetStateChannelDescriptor(), csReactor.SetStateChannel)
	node.router.AddChDescToBeAdded(consensus.GetDataChannelDescriptor(), csReactor.SetDataChannel)
	node.router.AddChDescToBeAdded(consensus.GetVoteChannelDescriptor(), csReactor.SetVoteChannel)
	node.router.AddChDescToBeAdded(consensus.GetVoteSetChannelDescriptor(), csReactor.SetVoteSetChannel)
	node.services = append(node.services, csReactor)
	node.rpcEnv.ConsensusReactor = csReactor

	// Create the blockchain reactor. Note, we do not start block sync if we're
	// doing a state sync first.
	bcReactor := blocksync.NewReactor(
		logger.With("module", "blockchain"),
		stateStore,
		blockExec,
		blockStore,
		csReactor,
		peerManager.Subscribe,
		peerManager,
		blockSync && !stateSync && !shoulddbsync,
		nodeMetrics.consensus,
		eventBus,
		restartCh,
		cfg.SelfRemediation,
	)
	node.router.AddChDescToBeAdded(blocksync.GetChannelDescriptor(), bcReactor.SetChannel)
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
		pxReactor := pex.NewReactor(
			logger,
			peerManager,
			peerManager.Subscribe,
			restartCh,
			cfg.SelfRemediation,
		)
		node.services = append(node.services, pxReactor)
		node.router.AddChDescToBeAdded(pex.ChannelDescriptor(), pxReactor.SetChannel)
	}

	postSyncHook := func(ctx context.Context, state sm.State) error {
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
	ssReactor := statesync.NewReactor(
		genDoc.ChainID,
		genDoc.InitialHeight,
		*cfg.StateSync,
		logger.With("module", "statesync"),
		proxyApp,
		peerManager.Subscribe,
		stateStore,
		blockStore,
		cfg.StateSync.TempDir,
		nodeMetrics.statesync,
		eventBus,
		// the post-sync operation
		postSyncHook,
		stateSync,
		restartCh,
		cfg.SelfRemediation,
	)

	node.shouldHandshake = !stateSync && !shoulddbsync
	node.services = append(node.services, ssReactor)
	node.router.AddChDescToBeAdded(statesync.GetSnapshotChannelDescriptor(), ssReactor.SetSnapshotChannel)
	node.router.AddChDescToBeAdded(statesync.GetChunkChannelDescriptor(), ssReactor.SetChunkChannel)
	node.router.AddChDescToBeAdded(statesync.GetLightBlockChannelDescriptor(), ssReactor.SetLightBlockChannel)
	node.router.AddChDescToBeAdded(statesync.GetParamsChannelDescriptor(), ssReactor.SetParamsChannel)

	dbsyncReactor := dbsync.NewReactor(
		logger.With("module", "dbsync"),
		*cfg.DBSync,
		cfg.BaseConfig,
		peerManager.Subscribe,
		stateStore,
		blockStore,
		genDoc.InitialHeight,
		genDoc.ChainID,
		eventBus,
		shoulddbsync,
		func(ctx context.Context, state sm.State) error {
			if _, err := client.LoadLatest(ctx, &abci.RequestLoadLatest{}); err != nil {
				return err
			}
			mpReactor.MarkReadyToStart()
			return postSyncHook(ctx, state)
		},
	)
	node.services = append(node.services, dbsyncReactor)
	node.router.AddChDescToBeAdded(dbsync.GetMetadataChannelDescriptor(), dbsyncReactor.SetMetadataChannel)
	node.router.AddChDescToBeAdded(dbsync.GetFileChannelDescriptor(), dbsyncReactor.SetFileChannel)
	node.router.AddChDescToBeAdded(dbsync.GetLightBlockChannelDescriptor(), dbsyncReactor.SetLightBlockChannel)
	node.router.AddChDescToBeAdded(dbsync.GetParamsChannelDescriptor(), dbsyncReactor.SetParamsChannel)

	if cfg.Mode == config.ModeValidator {
		if privValidator != nil {
			csState.SetPrivValidator(ctx, privValidator)
		}
	}
	node.rpcEnv.PubKey = pubKey

	node.BaseService = *service.NewBaseService(logger, "Node", node)

	return node, nil
}

// OnStart starts the Node. It implements service.Service.
func (n *nodeImpl) OnStart(ctx context.Context) error {
	if err := n.rpcEnv.ProxyApp.Start(ctx); err != nil {
		return fmt.Errorf("error starting proxy app connections: %w", err)
	}

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
		if err := consensus.NewHandshaker(n.logger.With("module", "handshaker"),
			n.stateStore, n.initialState, n.blockStore, n.rpcEnv.EventBus, n.genesisDoc,
		).Handshake(ctx, n.rpcEnv.ProxyApp); err != nil {
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

	logNodeStartupInfo(state, n.rpcEnv.PubKey, n.logger, n.config.Mode)

	// TODO: Fetch and provide real options and do proper p2p bootstrapping.
	// TODO: Use a persistent peer database.
	n.nodeInfo, err = makeNodeInfo(n.config, n.nodeKey, n.eventSinks, n.genesisDoc, state.Version.Consensus)
	if err != nil {
		return err
	}
	// Start Internal Services

	if n.config.RPC.PprofListenAddress != "" {
		signal := make(chan struct{})
		srv := &http.Server{Addr: n.config.RPC.PprofListenAddress, Handler: nil}
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
			n.logger.Info("Starting pprof server", "laddr", n.config.RPC.PprofListenAddress)

			if err := srv.ListenAndServe(); err != nil {
				n.logger.Error("pprof server error", "err", err)
				close(signal)
			}
		}()
	}

	now := tmtime.Now()
	genTime := n.genesisDoc.GenesisTime
	if genTime.After(now) {
		n.logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)

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
	n.logger.Info("Stopping Node")
	// stop the listeners / external services first
	for _, l := range n.rpcListeners {
		n.logger.Info("Closing rpc listener", "listener", l)
		if err := l.Close(); err != nil {
			n.logger.Error("error closing listener", "listener", l, "err", err)
		}
	}

	for _, es := range n.eventSinks {
		if err := es.Stop(); err != nil {
			n.logger.Error("failed to stop event sink", "err", err)
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
			n.logger.Error("Prometheus HTTP server Shutdown", "err", err)
		}

	}
	if err := n.shutdownOps(); err != nil {
		if strings.TrimSpace(err.Error()) != "" {
			n.logger.Error("problem shutting down additional services", "err", err)
		}
	}
	if n.blockStore != nil {
		if err := n.blockStore.Close(); err != nil {
			n.logger.Error("problem closing blockstore", "err", err)
		}
	}
	if n.stateStore != nil {
		if err := n.stateStore.Close(); err != nil {
			n.logger.Error("problem closing statestore", "err", err)
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
			n.logger.Error("Prometheus HTTP server ListenAndServe", "err", err)
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

func getRouterConfig(conf *config.Config, appClient abciclient.Client) p2p.RouterOptions {
	opts := p2p.RouterOptions{}

	if conf.FilterPeers && appClient != nil {
		opts.FilterPeerByID = func(ctx context.Context, id types.NodeID) error {
			res, err := appClient.Query(ctx, &abci.RequestQuery{
				Path: fmt.Sprintf("/p2p/filter/id/%s", id),
			})
			if err != nil {
				return err
			}
			if res.IsErr() {
				return fmt.Errorf("error querying abci app: %v", res)
			}

			return nil
		}

		opts.FilterPeerByIP = func(ctx context.Context, addrPort netip.AddrPort) error {
			res, err := appClient.Query(ctx, &abci.RequestQuery{
				Path: fmt.Sprintf("/p2p/filter/addr/%v", addrPort),
			})
			if err != nil {
				return err
			}
			if res.IsErr() {
				return fmt.Errorf("error querying abci app: %v", res)
			}

			return nil
		}

	}

	return opts
}
