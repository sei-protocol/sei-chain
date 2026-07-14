package node

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // nolint: gosec // securely exposed on separate, optional port
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/protobuf/proto"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
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
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"

	_ "github.com/grafana/pyroscope-go/godeltaprof/http/pprof"

	_ "github.com/lib/pq" // provide the psql db driver
)

type chainIDGatherer struct{ chainID string }

func (g chainIDGatherer) Gather() ([]*dto.MetricFamily, error) {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, err
	}
	for _, metricFamily := range metricFamilies {
		for _, metric := range metricFamily.Metric {
			if hasMetricLabel(metric, "chain_id") {
				continue
			}
			labels := slices.Clone(metric.Label)
			labels = append(labels, &dto.LabelPair{
				Name:  proto.String("chain_id"),
				Value: proto.String(g.chainID),
			})
			slices.SortFunc(labels, func(a, b *dto.LabelPair) int {
				return strings.Compare(a.GetName(), b.GetName())
			})
			metric.Label = labels
		}
	}
	return metricFamilies, nil
}

func hasMetricLabel(metric *dto.Metric, name string) bool {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return true
		}
	}
	return false
}

// nodeImpl is the highest level interface to a full Tendermint node.
// It includes all configuration information and running services.
type nodeImpl struct {
	service.BaseService

	// config
	config          *config.Config
	genesisDoc      *types.GenesisDoc   // initial validator set
	privValidator   types.PrivValidator // local node's validator key
	shouldHandshake bool                // set during makeNode
	consensusPolicy types.ConsensusPolicy

	// network
	router               *p2p.Router
	giga                 utils.Option[p2p.GigaRouter]
	gigaBlockDB          utils.Option[atypes.BlockDB] // owned here; closed after giga.Run (sync.Once)
	gigaBlockDBCloseOnce sync.Once
	ServiceRestartCh     utils.Option[chan []string]
	nodeInfo             types.NodeInfo
	nodeKey              types.NodeKey // our node privkey

	// services
	eventSinks     []indexer.EventSink
	initialState   sm.State
	stateStore     sm.Store
	blockStore     *store.BlockStore // store the blockchain to disk
	mempool        utils.Option[*mempool.TxMempool]
	evPool         utils.Option[*evidence.Pool]
	indexerService *indexer.Service
	services       []service.Service
	rpcListeners   []net.Listener // rpc servers
	shutdownOps    closer
	rpcEnv         *rpccore.Environment
	prometheusSrv  utils.Option[*http.Server]
}

// makeNode returns a new, ready to go, Tendermint Node.
func makeNode(
	ctx context.Context,
	cfg *config.Config,
	restartEvent func(),
	filePrivval *privval.FilePV,
	nodeKey types.NodeKey,
	proxyApp *proxy.Proxy,
	genesisDocProvider genesisDocProvider,
	dbProvider config.DBProvider,
	tracerProviderOptions []trace.TracerProviderOption,
	consensusPolicy types.ConsensusPolicy,
) (_ local.NodeService, err error) {
	var (
		cancel context.CancelFunc
		node   *nodeImpl
	)
	ctx, cancel = context.WithCancel(ctx)
	closers := []closer{convertCancelCloser(cancel)}
	defer func() {
		if err != nil {
			// Close BlockDB on construct failure after it was opened. Must not
			// live in shutdownOps (see OnStart comment on SpawnCritical).
			if node != nil {
				_ = node.closeGigaBlockDB()
			}
			err = combineCloseError(err, makeCloser(closers))
		}
	}()
	blockStore, stateDB, dbCloser, err := initDBs(cfg, dbProvider)
	closers = append(closers, dbCloser)
	if err != nil {
		return nil, fmt.Errorf("initDBs(): %w", err)
	}

	stateStore := sm.NewStore(stateDB)

	genDoc, err := genesisDocProvider()
	if err != nil {
		return nil, fmt.Errorf("genesisDocProvider(): %w", err)
	}

	if err = genDoc.ValidateAndComplete(); err != nil {
		return nil, fmt.Errorf("error in genesis doc: %w", err)
	}

	state, err := LoadStateFromDBOrGenesisDocProvider(stateStore, genDoc)
	if err != nil {
		return nil, fmt.Errorf("LoadStateFromDBOrGenesisDocProvider(): %w", err)
	}

	eventBus := eventbus.NewDefault()

	var eventLog *eventlog.Log
	if w := cfg.RPC.EventLogWindowSize; w > 0 {
		var err error
		eventLog, err = eventlog.New(eventlog.LogSettings{
			WindowSize: w,
			MaxItems:   cfg.RPC.EventLogMaxItems,
		})
		if err != nil {
			return nil, fmt.Errorf("initializing event log: %w", err)
		}
	}
	eventSinks, err := sink.EventSinksFromConfig(cfg, dbProvider, genDoc.ChainID)
	if err != nil {
		return nil, fmt.Errorf("sink.EventSinksFromConfig(): %w", err)
	}
	indexerService := indexer.NewService(indexer.ServiceArgs{
		Sinks:    eventSinks,
		EventBus: eventBus,
	})

	privValidator, err := createPrivval(ctx, cfg, genDoc, filePrivval)
	if err != nil {
		return nil, fmt.Errorf("createPrivval(): %w", err)
	}

	pubKey := utils.None[crypto.PubKey]()
	if cfg.Mode == config.ModeValidator {
		key, err := privValidator.GetPubKey(ctx)
		if err != nil {
			return nil, fmt.Errorf("can't get pubkey: %w", err)
		}
		pubKey = utils.Some(key)
	}
	eventLogOpt := utils.None[*eventlog.Log]()
	if eventLog != nil {
		eventLogOpt = utils.Some(eventLog)
	}
	// TODO construct node here:
	node = &nodeImpl{
		config:          cfg,
		genesisDoc:      genDoc,
		privValidator:   privValidator,
		consensusPolicy: consensusPolicy,

		nodeKey: nodeKey,

		eventSinks:     eventSinks,
		indexerService: indexerService,
		services:       []service.Service{eventBus},

		initialState: state,
		stateStore:   stateStore,
		blockStore:   blockStore,

		rpcEnv: &rpccore.Environment{
			App: proxyApp,

			StateStore: stateStore,
			BlockStore: blockStore,
			GenDoc:     genDoc,
			EventSinks: eventSinks,
			EventBus:   eventBus,
			EventLog:   eventLogOpt,
			Config:     *cfg.RPC,
		},
	}

	gigaEnabled := cfg.AutobahnConfigFile != ""
	// Pass the local key when autobahn is on; setup.go's
	// buildGigaRouter picks validator-vs-fullnode by cfg.Mode and
	// uses the key to check that a validator-mode node is in the committee.
	gigaValidatorKey := utils.None[atypes.SecretKey]()
	if gigaEnabled {
		gigaValidatorKey = utils.Some(atypes.SecretKeyFromED25519(filePrivval.Key.PrivKey))
	}
	router, peerCloser, gigaBlockDB, err := createRouter(
		node.NodeInfo,
		nodeKey,
		gigaValidatorKey,
		cfg,
		utils.Some(proxyApp),
		genDoc,
		dbProvider,
	)
	closers = append(closers, peerCloser)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}
	node.router = router
	node.giga = router.Giga()
	node.gigaBlockDB = gigaBlockDB
	// BlockDB is NOT closed in OnStop: BaseService runs OnStop before
	// SpawnCritical (giga.Run) finishes, so closing there would race with
	// still-running persist/execute. Close paths:
	//   - makeNode defer on construct failure
	//   - OnStart defer if Start fails before giga is spawned
	//   - SpawnCritical wrapper after giga.Run returns (happy path / cancel)
	node.rpcEnv.Router = router

	evReactor, evPool, edbCloser, err := createEvidenceReactor(cfg, dbProvider,
		stateStore, blockStore, node.router, eventBus)
	closers = append(closers, edbCloser)
	if err != nil {
		return nil, fmt.Errorf("createEvidenceReactor(): %w", err)
	}
	node.services = append(node.services, evReactor)
	node.rpcEnv.EvidencePool = utils.Some[sm.EvidencePool](evPool)
	node.evPool = utils.Some(evPool)

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

	if !gigaEnabled {
		mp := mempool.NewTxMempool(cfg.Mempool.ToMempoolConfig(), proxyApp, sm.TxConstraintsFetcherFromStore(stateStore))
		node.mempool = utils.Some(mp)
		node.rpcEnv.Mempool = utils.Some(mp)
		mpReactor, err := mempoolreactor.NewReactor(cfg.Mempool, mp, router)
		if err != nil {
			return nil, fmt.Errorf("mempoolreactor.NewReactor(): %w", err)
		}
		mpReactor.MarkReadyToStart()
		node.services = append(node.services, mpReactor)

		// make block executor for consensus and blockchain reactors to execute blocks
		blockExec := sm.NewBlockExecutor(
			stateStore,
			proxyApp,
			mp,
			evPool,
			blockStore,
			eventBus,
			consensusPolicy,
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
		waitSync := stateSync || blockSync

		consensusWAL, err := consensus.OpenWAL(cfg.Consensus.WalFile())
		if err != nil {
			return nil, fmt.Errorf("consensus.OpenWAL(): %w", err)
		}
		closers = append(closers, func() error {
			consensusWAL.Close()
			return nil
		})
		csState := consensus.NewState(
			cfg.Consensus,
			consensusWAL,
			stateStore,
			blockExec,
			blockStore,
			mp,
			evPool,
			eventBus,
			tracerProviderOptions,
		)
		node.rpcEnv.ConsensusState = utils.Some[rpccore.ConsensusState](csState)

		csReactor, err := consensus.NewReactor(
			csState,
			node.router,
			eventBus,
			waitSync,
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf("consensus.NewReactor(): %w", err)
		}

		node.services = append(node.services, csReactor)
		node.rpcEnv.ConsensusReactor = utils.Some(csReactor)

		// Create the blockchain reactor. Note, we do not start block sync if we're
		// doing a state sync first.
		bcReactor, err := blocksync.NewReactor(
			stateStore,
			blockStore,
			node.router,
			utils.Some(blocksync.SyncerConfig{
				BlockExec:             blockExec,
				ConsReactor:           utils.Some[blocksync.ConsensusReactor](csReactor),
				BlockSync:             blockSync && !stateSync,
				EventBus:              eventBus,
				RestartEvent:          restartEvent,
				SelfRemediationConfig: cfg.SelfRemediation,
			}),
		)
		if err != nil {
			return nil, fmt.Errorf("blocksync.NewReactor(): %w", err)
		}
		node.services = append(node.services, bcReactor)
		node.rpcEnv.BlockSyncReactor = utils.Some(bcReactor)

		// Make ConsensusReactor. Don't enable fully if doing a state sync and/or block sync first.
		// FIXME We need to update metrics here, since other reactors don't have access to them.
		if stateSync {
			consensus.Global.StateSyncingAt().Set(1)
		} else if blockSync {
			consensus.Global.BlockSyncingAt().Set(1)
		}

		postSyncHook := func(ctx context.Context, state sm.State) error {
			csReactor.SetStateSyncingMetrics(0)

			// TODO: Some form of orchestrator is needed here between the state
			// advancing reactors to be able to control which one of the three
			// is running
			// FIXME Very ugly to have these metrics bleed through here.
			csReactor.SetBlockSyncingMetrics(1)
			if err := bcReactor.SwitchToBlockSync(state); err != nil {
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
		node.shouldHandshake = !stateSync
		ssReactor, err := statesync.NewReactor(
			genDoc.ChainID,
			genDoc.InitialHeight,
			*cfg.StateSync,
			proxyApp,
			node.router,
			stateStore,
			blockStore,
			cfg.StateSync.TempDir,
			eventBus,
			// the post-sync operation
			postSyncHook,
			stateSync,
			restartEvent,
			cfg.SelfRemediation,
		)
		if err != nil {
			return nil, fmt.Errorf("statesync.NewReactor(): %w", err)
		}
		node.services = append(node.services, ssReactor)

		if cfg.Mode == config.ModeValidator {
			if privValidator != nil {
				csState.SetPrivValidator(ctx, utils.Some(privValidator))
			}
		}
	} else {
		bcReactor, err := blocksync.NewReactor(
			stateStore,
			blockStore,
			node.router,
			utils.None[blocksync.SyncerConfig](),
		)
		if err != nil {
			return nil, fmt.Errorf("blocksync.NewReactor(): %w", err)
		}
		node.rpcEnv.BlockSyncReactor = utils.Some(bcReactor)
		node.services = append(node.services, bcReactor)
	}

	node.rpcEnv.PubKey = pubKey

	node.BaseService = *service.NewBaseService("Node", node)
	node.shutdownOps = makeCloser(closers)

	return node, nil
}

// OnStart starts the Node. It implements service.Service.
func (n *nodeImpl) OnStart(ctx context.Context) (err error) {
	// If Start fails before giga is spawned, BaseService does not call OnStop
	// and never cancels SpawnCritical — so BlockDB would otherwise leak.
	// When giga has already been spawned, its wrapper closes BlockDB after
	// Run observes the service-context cancel issued once OnStart returns.
	gigaSpawned := false
	defer func() {
		if err == nil || gigaSpawned {
			return
		}
		_ = n.closeGigaBlockDB()
	}()

	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped or crashed after it saved the block
	// but before it indexed the txs)
	if err = n.rpcEnv.EventBus.Start(ctx); err != nil {
		return err
	}

	if err = n.indexerService.Start(ctx); err != nil {
		return err
	}

	// state sync will cover initialization the chain. Also calling InitChain isn't safe
	// when there is state sync as InitChain itself doesn't commit application state which
	// would get mixed up with application state writes by state sync.
	if n.shouldHandshake {
		// Create the handshaker, which calls RequestInfo, sets the AppVersion on the state,
		// and replays any blocks as necessary to sync tendermint with the app.
		if err = consensus.NewHandshaker(
			n.stateStore, n.initialState, n.blockStore, n.rpcEnv.EventBus, n.genesisDoc, n.consensusPolicy,
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
	if evPool, ok := n.evPool.Get(); ok {
		if err := evPool.Start(state); err != nil {
			return err
		}
	}

	if n.config.Instrumentation.Prometheus && n.config.Instrumentation.PrometheusListenAddr != "" {
		n.prometheusSrv = utils.Some(n.startPrometheusServer(ctx, n.config.Instrumentation.PrometheusListenAddr))
	}

	// Start the transport.
	if err = n.router.Start(ctx); err != nil {
		return err
	}
	n.rpcEnv.IsListening = true
	if m, ok := n.mempool.Get(); ok {
		n.SpawnCritical("mempool", m.Run)
	}
	// Run the GigaRouter alongside the transport. n.giga is the canonical
	// reference; the Router holds a copy only for its own internal use
	// (dispatching inbound giga connections). Lifecycle is owned here:
	// BlockDB was opened in setup and is closed after Run returns so we do not
	// race BaseService's OnStop-before-wg.Wait ordering.
	if giga, ok := n.giga.Get(); ok {
		gigaSpawned = true
		n.SpawnCritical("giga", func(ctx context.Context) error {
			defer func() { _ = n.closeGigaBlockDB() }()
			return giga.Run(ctx)
		})
	}

	for _, reactor := range n.services {
		if err = reactor.Start(ctx); err != nil {
			return fmt.Errorf("problem starting service '%T': %w ", reactor, err)
		}
	}

	n.rpcEnv.NodeInfo = n.nodeInfo
	// Start the RPC server before the P2P server
	// so we can eg. receive txs for the first block
	if n.config.RPC.ListenAddress != "" {
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

	if srv, ok := n.prometheusSrv.Get(); ok {
		if err := srv.Shutdown(context.Background()); err != nil {
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

// closeGigaBlockDB closes the Autobahn BlockDB at most once. Safe to call from
// makeNode's failure defer, OnStart's pre-giga failure path, and the giga
// SpawnCritical wrapper.
func (n *nodeImpl) closeGigaBlockDB() error {
	var err error
	n.gigaBlockDBCloseOnce.Do(func() {
		if db, ok := n.gigaBlockDB.Get(); ok {
			if err = db.Close(); err != nil {
				logger.Error("failed to close Autobahn BlockDB", "err", err)
			}
		}
	})
	return err
}

// startPrometheusServer starts a Prometheus HTTP server, listening for metrics
// collectors on addr.
func (n *nodeImpl) startPrometheusServer(ctx context.Context, addr string) *http.Server {
	gatherer := chainIDGatherer{
		chainID: n.genesisDoc.ChainID,
	}

	srv := &http.Server{
		Addr: addr,
		Handler: promhttp.InstrumentMetricHandler(
			prometheus.DefaultRegisterer, promhttp.HandlerFor(
				gatherer,
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
