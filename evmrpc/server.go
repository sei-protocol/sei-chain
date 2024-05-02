package evmrpc

import (
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/tendermint/tendermint/libs/log"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type ConnectionType string

var ConnectionTypeWS ConnectionType = "websocket"
var ConnectionTypeHTTP ConnectionType = "http"

const LocalAddress = "0.0.0.0"

type EVMServer interface {
	Start() error
}

func NewEVMHTTPServer(
	logger log.Logger,
	config Config,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfig client.TxConfig,
	homeDir string,
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
	sendAPI := NewSendAPI(tmClient, txConfig, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, ConnectionTypeHTTP)
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfig, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfig, homeDir, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), homeDir, ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   sendAPI,
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfig.TxDecoder(), tmClient, simulateConfig, ConnectionTypeHTTP),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), ConnectionTypeHTTP),
		},
		{
			Namespace: "eth",
			Service:   NewFilterAPI(tmClient, &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider}, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeHTTP),
		},
		{
			Namespace: "sei",
			Service:   NewAssociationAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), sendAPI, ConnectionTypeHTTP),
		},
		{
			Namespace: "txpool",
			Service:   NewTxPoolAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), &TxPoolConfig{maxNumTxs: int(config.MaxTxPoolTxs)}, ConnectionTypeHTTP),
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
		{
			Namespace: "debug",
			Service:   NewDebugAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), simulateConfig, ConnectionTypeHTTP),
		},
	}
	if err := httpServer.EnableRPC(apis, HTTPConfig{
		CorsAllowedOrigins: strings.Split(config.CORSOrigins, ","),
		Vhosts:             []string{"*"},
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
	ctxProvider func(int64) sdk.Context,
	txConfig client.TxConfig,
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
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfig, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfig, homeDir, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), homeDir, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfig, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig, ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, txConfig.TxDecoder(), tmClient, simulateConfig, ConnectionTypeWS),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), ConnectionTypeWS),
		},
		{
			Namespace: "eth",
			Service:   NewSubscriptionAPI(tmClient, &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider}, &SubscriptionConfig{subscriptionCapacity: 100, newHeadLimit: config.MaxSubscriptionsNewHead}, &FilterConfig{timeout: config.FilterTimeout, maxLog: config.MaxLogNoBlock, maxBlock: config.MaxBlocksForLog}, ConnectionTypeWS),
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
	}
	if err := httpServer.EnableWS(apis, WsConfig{Origins: strings.Split(config.WSOrigins, ",")}); err != nil {
		return nil, err
	}
	return httpServer, nil
}
