package evmrpc

import (
	"context"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/evmrpc/stats"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	evmCfg "github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type ConnectionType string

var ConnectionTypeWS ConnectionType = "websocket"
var ConnectionTypeHTTP ConnectionType = "http"

const LocalAddress = "0.0.0.0"
const DefaultWebsocketMaxMessageSize = 10 * 1024 * 1024

type EVMServer interface {
	Start() error
	Stop()
}

func NewEVMHTTPServer(
	logger log.Logger,
	config evmrpcconfig.Config,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	stateStore db_engine.StateStore,
	isPanicOrSyntheticTxFunc func(ctx context.Context, hash common.Hash) (bool, error), // used in *ExcludeTraceFail endpoints
) (EVMServer, error) {
	logger = logger.With("module", "evmrpc")

	// Initialize global worker pool with configuration (metrics are embedded in pool)
	InitGlobalWorkerPool(config.WorkerPoolSize, config.WorkerQueueSize)

	// Get pool for logging and DB semaphore setup
	pool := GetGlobalWorkerPool()
	workerCount := pool.WorkerCount()
	queueSize := pool.QueueSize()

	// Set DB semaphore capacity in metrics (aligned with worker count)
	// Only set once to avoid races when multiple test servers start in parallel.
	pool.Metrics.DBSemaphoreCapacity.CompareAndSwap(0, int32(workerCount)) //nolint:gosec // G115: safe, max is 64

	debugEnabled := IsDebugMetricsEnabled()
	logger.Info("Started EVM RPC metrics exporter (interval: 5s)", "workers", workerCount, "queue", queueSize, "db_semaphore", workerCount, "debug_stdout", debugEnabled)
	if !debugEnabled {
		logger.Info("To enable debug metrics output to stdout, set EVM_DEBUG_METRICS=true")
	}

	// Initialize RPC tracker
	stats.InitRPCTracker(ctxProvider(LatestCtxHeight).Context(), logger, config.RPCStatsInterval)

	httpServer := NewHTTPServer(logger, rpc.HTTPTimeouts{
		ReadTimeout:       config.ReadTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
	})
	if err := httpServer.SetListenAddr(LocalAddress, config.HTTPPort); err != nil {
		return nil, err
	}
	simulateConfig := &SimulateConfig{
		GasCap:                       config.SimulationGasLimit,
		EVMTimeout:                   config.SimulationEVMTimeout,
		MaxConcurrentSimulationCalls: config.MaxConcurrentSimulationCalls,
	}
	watermarks := NewWatermarkManager(tmClient, ctxProvider, stateStore, k.ReceiptStore())

	globalBlockCache := NewBlockCache(3000)
	cacheCreationMutex := &sync.Mutex{}
	sendAPI := NewSendAPI(tmClient, txConfigProvider, &SendConfig{slow: config.Slow}, k, beginBlockKeepers, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex, watermarks)

	ctx := ctxProvider(LatestCtxHeight)
	txAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP, watermarks, globalBlockCache, cacheCreationMutex)
	debugAPI := NewDebugAPI(tmClient, k, beginBlockKeepers, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex, watermarks)
	if isPanicOrSyntheticTxFunc == nil {
		isPanicOrSyntheticTxFunc = func(ctx context.Context, hash common.Hash) (bool, error) {
			return debugAPI.isPanicOrSyntheticTx(ctx, hash)
		}
	}
	seiTxAPI := NewSeiTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, watermarks, globalBlockCache, cacheCreationMutex)
	seiDebugAPI := NewSeiDebugAPI(tmClient, k, beginBlockKeepers, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex, watermarks)

	// DB semaphore aligned with worker count
	dbReadSemaphore := make(chan struct{}, workerCount)
	globalLogSlicePool := NewLogSlicePool()
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "sei",
			Service:   NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "sei2",
			Service:   NewSei2BlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   txAPI,
		},
		{
			Namespace: "sei",
			Service:   seiTxAPI,
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeHTTP, txConfigProvider(LatestCtxHeight).TxDecoder(), watermarks),
		},
		{
			Namespace: "eth",
			Service:   sendAPI,
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service: NewFilterAPI(
				tmClient,
				k,
				ctxProvider,
				txConfigProvider,
				&FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog},
				ConnectionTypeHTTP,
				"eth",
				dbReadSemaphore,
				globalBlockCache,
				cacheCreationMutex,
				globalLogSlicePool,
				watermarks,
			),
		},
		{
			Namespace: "sei",
			Service: NewFilterAPI(
				tmClient,
				k,
				ctxProvider,
				txConfigProvider,
				&FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog},
				ConnectionTypeHTTP,
				"sei",
				dbReadSemaphore,
				globalBlockCache,
				cacheCreationMutex,
				globalLogSlicePool,
				watermarks,
			),
		},
		{
			Namespace: "sei",
			Service:   NewAssociationAPI(tmClient, k, ctxProvider, txConfigProvider, sendAPI, ConnectionTypeHTTP, watermarks),
		},
		{
			Namespace: "txpool",
			Service:   NewTxPoolAPI(tmClient, k, ctxProvider, txConfigProvider, &TxPoolConfig{maxNumTxs: int(config.MaxTxPoolTxs)}, ConnectionTypeHTTP), //nolint:gosec
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
		{
			Namespace: "debug",
			Service:   debugAPI,
		},
		{
			Namespace: "sei",
			Service:   seiDebugAPI,
		},
	}
	// Test API can only exist on non-live chain IDs.  These APIs instrument certain overrides.
	if config.EnableTestAPI && !evmCfg.IsLiveChainID(ctx) {
		logger.Info("Enabling Test EVM APIs")
		apis = append(apis, rpc.API{
			Namespace: "test",
			Service:   NewTestAPI(),
		})
	} else {
		logger.Info("Disabling Test EVM APIs", "liveChainID", evmCfg.IsLiveChainID(ctx), "enableTestAPI", config.EnableTestAPI)
	}

	if err := httpServer.EnableRPC(apis, HTTPConfig{
		CorsAllowedOrigins: strings.Split(config.CORSOrigins, ","),
		Vhosts:             []string{"*"},
		DenyList:           config.DenyList,
	}); err != nil {
		return nil, err
	}

	return httpServer, nil
}

func NewEVMWebSocketServer(
	logger log.Logger,
	config evmrpcconfig.Config,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	homeDir string,
	stateStore db_engine.StateStore,
) (EVMServer, error) {
	logger = logger.With("module", "evmrpc")

	// Initialize global worker pool with configuration (metrics are embedded in pool)
	// This is idempotent - if HTTP server already initialized it, this is a no-op
	InitGlobalWorkerPool(config.WorkerPoolSize, config.WorkerQueueSize)

	// Initialize WebSocket tracker.
	stats.InitWSTracker(ctxProvider(LatestCtxHeight).Context(), logger, config.RPCStatsInterval)

	httpServer := NewHTTPServer(logger, rpc.HTTPTimeouts{
		ReadTimeout:       config.ReadTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
	})
	if err := httpServer.SetListenAddr(LocalAddress, config.WSPort); err != nil {
		return nil, err
	}
	simulateConfig := &SimulateConfig{
		GasCap:                       config.SimulationGasLimit,
		EVMTimeout:                   config.SimulationEVMTimeout,
		MaxConcurrentSimulationCalls: config.MaxConcurrentSimulationCalls,
	}
	watermarks := NewWatermarkManager(tmClient, ctxProvider, stateStore, k.ReceiptStore())
	// DB semaphore aligned with worker count
	dbReadSemaphore := make(chan struct{}, GetGlobalWorkerPool().WorkerCount())
	globalBlockCache := NewBlockCache(3000)
	cacheCreationMutex := &sync.Mutex{}
	globalLogSlicePool := NewLogSlicePool()
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeWS, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeWS, watermarks, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeWS, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeWS, txConfigProvider(LatestCtxHeight).TxDecoder(), watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfigProvider, &SendConfig{slow: config.Slow}, k, beginBlockKeepers, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeWS, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeWS, globalBlockCache, cacheCreationMutex, watermarks),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service: NewSubscriptionAPI(tmClient, k, ctxProvider, &LogFetcher{
				tmClient:           tmClient,
				k:                  k,
				ctxProvider:        ctxProvider,
				txConfigProvider:   txConfigProvider,
				dbReadSemaphore:    dbReadSemaphore,
				globalBlockCache:   globalBlockCache,
				cacheCreationMutex: cacheCreationMutex,
				globalLogSlicePool: globalLogSlicePool,
				watermarks:         watermarks,
			}, &SubscriptionConfig{subscriptionCapacity: 100, newHeadLimit: config.MaxSubscriptionsNewHead}, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeWS),
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
	}

	wsConfig := WsConfig{Origins: strings.Split(config.WSOrigins, ",")}
	wsConfig.readLimit = DefaultWebsocketMaxMessageSize
	if err := httpServer.EnableWS(apis, wsConfig); err != nil {
		return nil, err
	}

	return httpServer, nil
}
