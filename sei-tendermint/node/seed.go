package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/p2p/pex"
	"github.com/tendermint/tendermint/internal/proxy"
	rpccore "github.com/tendermint/tendermint/internal/rpc/core"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/state/indexer/sink"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	tmtime "github.com/tendermint/tendermint/libs/time"
	"github.com/tendermint/tendermint/types"
)

type seedNodeImpl struct {
	service.BaseService
	logger log.Logger

	// config
	config     *config.Config
	genesisDoc *types.GenesisDoc // initial validator set

	nodeInfo types.NodeInfo

	// network
	peerManager *p2p.PeerManager
	router      *p2p.Router
	nodeKey     types.NodeKey // our node privkey
	isListening bool

	// services
	pexReactor  service.Service // for exchanging peer addresses
	shutdownOps closer
	rpcEnv      *rpccore.Environment
}

// makeSeedNode returns a new seed node, containing only p2p, pex reactor
func makeSeedNode(
	ctx context.Context,
	logger log.Logger,
	cfg *config.Config,
	restartCh chan struct{},
	dbProvider config.DBProvider,
	nodeKey types.NodeKey,
	genesisDocProvider genesisDocProvider,
	client abciclient.Client,
	nodeMetrics *NodeMetrics,
) (service.Service, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

	// Setup Transport and Switch.
	peerManager, peerCloser, err := createPeerManager(logger, cfg, dbProvider, nodeKey.ID, nodeMetrics.p2p)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create peer manager: %w", err),
			peerCloser)
	}

	router, err := createRouter(logger, nodeMetrics.p2p, func() *types.NodeInfo { return &nodeInfo }, nodeKey, peerManager, cfg, nil)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create router: %w", err),
			peerCloser)
	}
	// Register a listener to restart router if signalled to do so
	go func() {
		for {
			select {
			case <-restartCh:
				logger.Info("Received signal to restart router, restarting...")
				router.OnStop()
				router.Wait()
				logger.Info("Router successfully stopped. Restarting...")
				// Start the transport.
				if err := router.Start(ctx); err != nil {
					logger.Error("Unable to start router, retrying...", err)
				}
			}
		}
	}()

	pexReactor := pex.NewReactor(
		logger,
		peerManager,
		peerManager.Subscribe,
		restartCh,
		cfg.SelfRemediation,
	)

	proxyApp := proxy.New(client, logger.With("module", "proxy"), nodeMetrics.proxy)

	closers := []closer{convertCancelCloser(cancel)}
	blockStore, stateDB, dbCloser, err := initDBs(cfg, dbProvider)
	if err != nil {
		return nil, combineCloseError(err, dbCloser)
	}
	closers = append(closers, dbCloser)

	eventSinks, err := sink.EventSinksFromConfig(cfg, dbProvider, genDoc.ChainID)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	eventBus := eventbus.NewDefault(logger.With("module", "events"))

	stateStore := sm.NewStore(stateDB)

	node := &seedNodeImpl{
		config:     cfg,
		logger:     logger,
		genesisDoc: genDoc,

		nodeKey:     nodeKey,
		peerManager: peerManager,
		router:      router,

		shutdownOps: peerCloser,

		pexReactor: pexReactor,
		rpcEnv: &rpccore.Environment{
			ProxyApp: proxyApp,

			StateStore: stateStore,
			BlockStore: blockStore,

			PeerManager: peerManager,

			GenDoc:     genDoc,
			EventSinks: eventSinks,
			EventBus:   eventBus,
			Logger:     logger.With("module", "rpc"),
			Config:     *cfg.RPC,
		},
		nodeInfo: nodeInfo,
	}
	node.router.AddChDescToBeAdded(pex.ChannelDescriptor(), pexReactor.SetChannel)
	node.BaseService = *service.NewBaseService(logger, "SeedNode", node)

	return node, nil
}

// OnStart starts the Seed Node. It implements service.Service.
func (n *seedNodeImpl) OnStart(ctx context.Context) error {

	if n.config.RPC.PprofListenAddress != "" {
		rpcCtx, rpcCancel := context.WithCancel(ctx)
		srv := &http.Server{Addr: n.config.RPC.PprofListenAddress, Handler: nil}
		go func() {
			select {
			case <-ctx.Done():
				sctx, scancel := context.WithTimeout(context.Background(), time.Second)
				defer scancel()
				_ = srv.Shutdown(sctx)
			case <-rpcCtx.Done():
			}
		}()

		go func() {
			n.logger.Info("Starting pprof server", "laddr", n.config.RPC.PprofListenAddress)

			if err := srv.ListenAndServe(); err != nil {
				n.logger.Error("pprof server error", "err", err)
				rpcCancel()
			}
		}()
	}

	now := tmtime.Now()
	genTime := n.genesisDoc.GenesisTime
	if genTime.After(now) {
		n.logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)
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
	n.logger.Info("Stopping Node")

	n.pexReactor.Wait()
	n.router.Wait()
	n.isListening = false

	if err := n.shutdownOps(); err != nil {
		if strings.TrimSpace(err.Error()) != "" {
			n.logger.Error("problem shutting down additional services", "err", err)
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
