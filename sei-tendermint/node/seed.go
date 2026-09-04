package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pex"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type seedNodeImpl struct {
	service.BaseService

	// config
	config     *config.Config
	genesisDoc *types.GenesisDoc // initial validator set

	nodeInfo types.NodeInfo

	// network
	router      *p2p.Router
	nodeKey     types.NodeKey // our node privkey
	isListening bool

	// services
	pexReactor    service.Service // for exchanging peer addresses
	shutdownOps   closer
	rpcEnv        *rpccore.Environment
	prometheusSrv utils.Option[*http.Server]
}

// makeSeedNode returns a new seed node, containing only p2p, pex reactor
func makeSeedNode(
	cfg *config.Config,
	dbProvider config.DBProvider,
	nodeKey types.NodeKey,
	genesisDocProvider genesisDocProvider,
) (_ local.NodeService, err error) {
	closers := []closer{}
	defer func() {
		if err != nil {
			err = combineCloseError(err, makeCloser(closers))
		}
	}()
	if !cfg.P2P.PexReactor {
		return nil, errors.New("cannot run seed nodes with PEX disabled")
	}

	genDoc, err := genesisDocProvider()
	if err != nil {
		return nil, err
	}

	state, err := sm.MakeGenesisState(genDoc)
	if err != nil {
		return nil, err
	}

	nodeInfo, err := makeSeedNodeInfo(cfg, nodeKey, genDoc, state)
	if err != nil {
		return nil, err
	}

	router, peerCloser, _, err := createRouter(
		func() *types.NodeInfo { return &nodeInfo },
		nodeKey,
		utils.None[atypes.SecretKey](),
		cfg,
		utils.None[*proxy.Proxy](),
		genDoc,
		dbProvider,
	)
	closers = append(closers, peerCloser)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	pexReactor, err := pex.NewReactor(router, pex.DefaultSendInterval)
	if err != nil {
		return nil, fmt.Errorf("pex.NewReactor(): %w", err)
	}

	blockStore, stateDB, dbCloser, err := initDBs(cfg, dbProvider)
	closers = append(closers, dbCloser)
	if err != nil {
		return nil, fmt.Errorf("initDBs: %w", err)
	}

	eventSinks, err := sink.EventSinksFromConfig(cfg, dbProvider, genDoc.ChainID)
	if err != nil {
		return nil, fmt.Errorf("sink.EventSinksFromConfig(): %w", err)
	}
	eventBus := eventbus.NewDefault()

	stateStore := sm.NewStore(stateDB)

	node := &seedNodeImpl{
		config:     cfg,
		genesisDoc: genDoc,

		nodeKey: nodeKey,
		router:  router,

		pexReactor: pexReactor,
		rpcEnv: &rpccore.Environment{
			App: proxy.New(abci.BaseApplication{}),

			StateStore: stateStore,
			BlockStore: blockStore,

			Router: router,

			GenDoc:     genDoc,
			EventSinks: eventSinks,
			EventBus:   eventBus,
			Config:     *cfg.RPC,
		},
		nodeInfo: nodeInfo,
	}
	node.BaseService = *service.NewBaseService("SeedNode", node)
	node.shutdownOps = makeCloser(closers)
	return node, nil
}

// OnStart starts the Seed Node. It implements service.Service.
func (n *seedNodeImpl) OnStart(ctx context.Context) (err error) {

	// A seed serves no RPC, so Prometheus is its only observability surface. The
	// p2p metrics it needs — peer count against the connection cap, and inbound
	// connection/handshake outcomes — register on the global registry at package
	// init and the router increments them regardless of mode, so exposing them
	// needs only this listener.
	//
	// First in OnStart, ahead of both pprof and the genesis-time wait. The wait is
	// an unbounded, non-ctx-aware sleep and the listener depends on nothing either
	// provides, so a seed that is unscrapeable across them is blind during exactly
	// the period an operator is asking whether it came up. Going first also means
	// the fatal return below cannot orphan the pprof listener, which has no
	// rollback of its own. The p2p series are already present by now — NewRouter
	// seeds them at construction — so scraping during the wait is not a window
	// where the endpoint answers but the gauges are missing.
	if n.config.Instrumentation.Prometheus && n.config.Instrumentation.PrometheusListenAddr != "" {
		// serr, not err: `err :=` here would shadow the named return, and the defer
		// below would then close over the shadowed copy — which is always nil by the
		// time it runs — silently disabling the teardown.
		srv, serr := startPrometheusServer(
			ctx,
			n.config.Instrumentation.PrometheusListenAddr,
			n.genesisDoc.ChainID,
			n.config.Instrumentation.MaxOpenConnections,
		)
		if serr != nil {
			// Fatal, where a full node logs and continues. Not because the failure
			// would otherwise be silent — a refused scrape trips the same target-down
			// alert, and the log line and pod state remain — but because a seed's
			// value is only realized when it is observable, and an unambiguous
			// CrashLoopBackOff is preferable to a seed that peers while blind. The
			// cost is real and deliberate: a transient conflict on this port stops
			// peer discovery, which is the seed's actual job.
			return fmt.Errorf("instrumentation.prometheus-listen-addr %q: %w",
				n.config.Instrumentation.PrometheusListenAddr, serr)
		}
		n.prometheusSrv = utils.Some(srv)

		// If Start fails from here on, BaseService does not call OnStop, and the
		// cancel it issues is of its own derived context, which this listener's
		// shutdown goroutine does not watch — so the port would stay bound with
		// nothing left to release it.
		//
		// The close is prompt but not synchronous with this return: on a fast
		// failure Shutdown can run before Serve has registered the listener, find
		// nothing tracked, and leave the close to Serve's own defer. Measured at a
		// few milliseconds — enough to release the port, not enough to rebind it
		// in the same breath.
		defer func() {
			if err == nil {
				return
			}
			shutdownPrometheus(srv)
		}()
	}

	if n.config.RPC.PprofListenAddress != "" {
		rpcCtx, rpcCancel := context.WithCancel(ctx)
		srv := &http.Server{
			Addr:              n.config.RPC.PprofListenAddress,
			Handler:           nil,
			ReadHeaderTimeout: 10 * time.Second, //nolint:gosec // G112: mitigate slowloris attacks
		}
		go func() {
			select {
			case <-ctx.Done():
				sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer scancel()
				_ = srv.Shutdown(sctx)
			case <-rpcCtx.Done():
			}
		}()

		go func() {
			logger.Info("Starting pprof server", "laddr", n.config.RPC.PprofListenAddress)

			if err := srv.ListenAndServe(); err != nil {
				logger.Error("pprof server error", "err", err)
				rpcCancel()
			}
		}()
	}

	now := tmtime.Now()
	genTime := n.genesisDoc.GenesisTime
	if genTime.After(now) {
		logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)
		time.Sleep(genTime.Sub(now))
	}

	// Start the transport.
	if err := n.router.Start(ctx); err != nil {
		return err
	}
	n.isListening = true

	if n.config.P2P.PexReactor {
		if err := n.pexReactor.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

// OnStop stops the Seed Node. It implements service.Service.
func (n *seedNodeImpl) OnStop() {
	logger.Info("Stopping Node")

	// Stop before Wait, as nodeImpl.OnStop does. Both were started with the outer
	// context, which BaseService.Stop does not cancel, so Wait on its own never
	// returns — and everything below it, including the listener teardown, never
	// runs.
	n.pexReactor.Stop()
	n.pexReactor.Wait()
	n.router.Stop()
	n.router.Wait()
	n.isListening = false

	if srv, ok := n.prometheusSrv.Get(); ok {
		shutdownPrometheus(srv)
	}

	if err := n.shutdownOps(); err != nil {
		if strings.TrimSpace(err.Error()) != "" {
			logger.Error("problem shutting down additional services", "err", err)
		}
	}
}

// EventBus returns the Node's EventBus.
func (n *seedNodeImpl) EventBus() *eventbus.EventBus {
	return n.rpcEnv.EventBus
}

// RPCEnvironment makes sure RPC has all the objects it needs to operate.
func (n *seedNodeImpl) RPCEnvironment() *rpccore.Environment {
	return n.rpcEnv
}

func (n *seedNodeImpl) NodeInfo() *types.NodeInfo {
	return &n.nodeInfo
}
