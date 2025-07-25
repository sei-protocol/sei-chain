package evmrpc

import (
	"context"
	"strings"

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
const DefaultWebsocketMaxMessageSize = 2 * 1024 * 1024

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
	simulateConfig := &SimulateConfig{GasCap: config.SimulationGasLimit, EVMTimeout: config.SimulationEVMTimeout}
	sendAPI := NewSendAPI(tmClient, txConfigProvider, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeHTTP)
	ctx := ctxProvider(LatestCtxHeight)

	txAPI := NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP)
	debugAPI := NewDebugAPI(tmClient, k, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config)
	if isPanicOrSyntheticTxFunc == nil {
		isPanicOrSyntheticTxFunc = func(ctx context.Context, hash common.Hash) (bool, error) {
			return debugAPI.isPanicOrSyntheticTx(ctx, hash)
		}
	}
	seiTxAPI := NewSeiTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc)
	seiDebugAPI := NewSeiDebugAPI(tmClient, k, ctxProvider, txConfigProvider, simulateConfig, app, antehandler, ConnectionTypeHTTP, config)

	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "sei",
			Service:   NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc),
		},
		{
			Namespace: "sei2",
			Service:   NewSei2BlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeHTTP, isPanicOrSyntheticTxFunc),
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
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   sendAPI,
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeHTTP),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewFilterAPI(tmClient, k, ctxProvider, txConfigProvider, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeHTTP, "eth"),
		},
		{
			Namespace: "sei",
			Service:   NewFilterAPI(tmClient, k, ctxProvider, txConfigProvider, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeHTTP, "sei"),
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
	simulateConfig := &SimulateConfig{GasCap: config.SimulationGasLimit, EVMTimeout: config.SimulationEVMTimeout}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfigProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfigProvider, homeDir, config.MaxBlocksForLog, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfigProvider, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, app, antehandler, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfigProvider, tmClient, simulateConfig, app, antehandler, ConnectionTypeWS),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSubscriptionAPI(tmClient, k, ctxProvider, &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider, txConfigProvider: txConfigProvider}, &SubscriptionConfig{subscriptionCapacity: 100, newHeadLimit: config.MaxSubscriptionsNewHead}, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeWS),
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
