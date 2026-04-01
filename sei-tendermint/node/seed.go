package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pex"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/sink"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
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
	pexReactor  service.Service // for exchanging peer addresses
	shutdownOps closer
	rpcEnv      *rpccore.Environment
}

// makeSeedNode returns a new seed node, containing only p2p, pex reactor
func makeSeedNode(
	cfg *config.Config,
	dbProvider config.DBProvider,
	nodeKey types.NodeKey,
	genesisDocProvider genesisDocProvider,
	nodeMetrics *NodeMetrics,
) (service.Service, error) {
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

	router, peerCloser, err := createRouter(nodeMetrics.p2p, func() *types.NodeInfo { return &nodeInfo }, nodeKey, cfg, nil, dbProvider)
	if err != nil {
		return nil, combineCloseError(
			fmt.Errorf("failed to create router: %w", err),
			peerCloser)
	}

	pexReactor, err := pex.NewReactor(router, pex.DefaultSendInterval)
	if err != nil {
		return nil, fmt.Errorf("pex.NewReactor(): %w", err)
	}

	closers := make([]closer, 0, 2)
	blockStore, stateDB, dbCloser, err := initDBs(cfg, dbProvider)
	if err != nil {
		return nil, combineCloseError(err, dbCloser)
	}
	closers = append(closers, dbCloser)

	eventSinks, err := sink.EventSinksFromConfig(cfg, dbProvider, genDoc.ChainID)
	if err != nil {
		return nil, combineCloseError(err, makeCloser(closers))
	}
	eventBus := eventbus.NewDefault()

	stateStore := sm.NewStore(stateDB)

	node := &seedNodeImpl{
		config:     cfg,
		genesisDoc: genDoc,

		nodeKey: nodeKey,
		router:  router,

		shutdownOps: peerCloser,

		pexReactor: pexReactor,
		rpcEnv: &rpccore.Environment{
			App: abci.NewBaseApplication(),

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

	return node, nil
}

// OnStart starts the Seed Node. It implements service.Service.
func (n *seedNodeImpl) OnStart(ctx context.Context) error {

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
				sctx, scancel := context.WithTimeout(context.Background(), time.Second)
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

	n.pexReactor.Wait()
	n.router.Wait()
	n.isListening = false

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
