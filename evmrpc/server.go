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

const LocalAddress = "127.0.0.1"

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
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfig),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfig, homeDir),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), homeDir),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfig, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, tmClient, simulateConfig),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, txConfig.TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewFilterAPI(tmClient, &FilterConfig{timeout: config.FilterTimeout}),
		},
		{
			Namespace: "txpool",
			Service:   NewTxPoolAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), &TxPoolConfig{maxNumTxs: int(config.MaxTxPoolTxs)}),
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
		},
	}
	if err := httpServer.EnableRPC(apis, HTTPConfig{
		CorsAllowedOrigins: strings.Split(config.CORSOrigins, ","),
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
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfig),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfig, homeDir),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfig.TxDecoder(), homeDir),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfig, &SendConfig{slow: config.Slow}, k, ctxProvider, homeDir, simulateConfig),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, tmClient, simulateConfig),
		},
		{
			Namespace: "net",
			Service:   NewNetAPI(tmClient, k, ctxProvider, txConfig.TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewSubscriptionAPI(tmClient, ctxProvider, &SubscriptionConfig{subscriptionCapacity: 100}),
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
