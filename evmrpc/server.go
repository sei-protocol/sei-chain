package evmrpc

import (
	"context"
	"strings"
	"sync"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	evmCfg "github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/tendermint/tendermint/libs/log"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
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
	config Config,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	earliestVersion func() int64,
	homeDir string,
	isPanicOrSyntheticTxFunc func(ctx context.Context, hash common.Hash) (bool, error), // used in *ExcludeTraceFail endpoints
) (EVMServer, error) {
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
	globalBlockCache := NewBlockCache(3000)
	cacheCreationMutex := &sync.Mutex{}
	sendAPI := NewSendAPI(tmClient, txConfigProvider, earliestVersion, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex)

	ctx := ctxProvider(LatestCtxHeight)

	txAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, homeDir, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex)
	debugAPI := NewDebugAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex)
	if isPanicOrSyntheticTxFunc == nil {
		isPanicOrSyntheticTxFunc = func(ctx context.Context, hash common.Hash) (bool, error) {
			return debugAPI.isPanicOrSyntheticTx(ctx, hash)
		}
	}
	seiTxAPI := NewSeiTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, homeDir, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, globalBlockCache, cacheCreationMutex)
	seiDebugAPI := NewSeiDebugAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, simulateConfig, app, antehandler, ConnectionTypeHTTP, config, globalBlockCache, cacheCreationMutex)

	dbReadSemaphore := make(chan struct{}, MaxDBReadConcurrency)
	globalLogSlicePool := NewLogSlicePool()
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "sei",
			Service:   NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "sei2",
			Service:   NewSei2BlockAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc, globalBlockCache, cacheCreationMutex),
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
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeHTTP, txConfigProvider(LatestCtxHeight).TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   sendAPI,
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfigProvider, earliestVersion, tmClient, simulateConfig, app, antehandler, ConnectionTypeHTTP, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewFilterAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeHTTP, "eth", dbReadSemaphore, globalBlockCache, cacheCreationMutex, globalLogSlicePool),
		},
		{
			Namespace: "sei",
			Service:   NewFilterAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeHTTP, "sei", dbReadSemaphore, globalBlockCache, cacheCreationMutex, globalLogSlicePool),
		},
		{
			Namespace: "sei",
			Service:   NewAssociationAPI(tmClient, k, ctxProvider, txConfigProvider, sendAPI, ConnectionTypeHTTP),
		},
		{
			Namespace: "txpool",
			Service:   NewTxPoolAPI(tmClient, k, ctxProvider, txConfigProvider, &TxPoolConfig{maxNumTxs: int(config.MaxTxPoolTxs)}, ConnectionTypeHTTP),
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
	config Config,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	earliestVersion func() int64,
	homeDir string,
) (EVMServer, error) {
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
	dbReadSemaphore := make(chan struct{}, MaxDBReadConcurrency)
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
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, ConnectionTypeWS, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, earliestVersion, homeDir, ConnectionTypeWS, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeWS, txConfigProvider(LatestCtxHeight).TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfigProvider, earliestVersion, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeWS, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfigProvider, earliestVersion, tmClient, simulateConfig, app, antehandler, ConnectionTypeWS, globalBlockCache, cacheCreationMutex),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service: NewSubscriptionAPI(
				tmClient,
				k,
				ctxProvider,
				&LogFetcher{
					tmClient:           tmClient,
					k:                  k,
					ctxProvider:        ctxProvider,
					txConfigProvider:   txConfigProvider,
					earliestVersion:    earliestVersion,
					dbReadSemaphore:    dbReadSemaphore,
					globalBlockCache:   globalBlockCache,
					cacheCreationMutex: cacheCreationMutex,
					globalLogSlicePool: globalLogSlicePool,
				},
				&SubscriptionConfig{subscriptionCapacity: 100, newHeadLimit: config.MaxSubscriptionsNewHead},
				&FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog},
				ConnectionTypeWS,
			),
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
