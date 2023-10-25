package evmrpc

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/tendermint/tendermint/libs/log"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type EVMServer interface {
	Start() error
}

func NewEVMHTTPServer(
	logger log.Logger,
	addr string,
	port int,
	timeouts rpc.HTTPTimeouts,
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfig client.TxConfig,
	simulationConfig *SimulateConfig,
) (EVMServer, error) {
	httpServer := newHTTPServer(logger, timeouts)
	if err := httpServer.setListenAddr(addr, port); err != nil {
		return nil, err
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
		{
			Namespace: "eth",
			Service:   NewBlockAPI(tmClient, k, ctxProvider, txConfig.TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewTransactionAPI(tmClient, k, ctxProvider, txConfig.TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewStateAPI(tmClient, k, ctxProvider),
		},
		{
			Namespace: "eth",
			Service:   NewInfoAPI(tmClient, k, ctxProvider, txConfig.TxDecoder()),
		},
		{
			Namespace: "eth",
			Service:   NewSendAPI(tmClient, txConfig),
		},
		{
			Namespace: "eth",
			Service:   NewSimulationAPI(ctxProvider, k, tmClient, simulationConfig),
		},
	}
	if err := httpServer.enableRPC(apis, httpConfig{
		// TODO: add CORS configs and virtual host configs
	}); err != nil {
		return nil, err
	}
	return httpServer, nil
}

func NewEVMWebSocketServer(
	logger log.Logger,
	addr string,
	port int,
	origins []string,
	timeouts rpc.HTTPTimeouts,
) (EVMServer, error) {
	httpServer := newHTTPServer(logger, timeouts)
	if err := httpServer.setListenAddr(addr, port); err != nil {
		return nil, err
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
	}
	if err := httpServer.enableWS(apis, wsConfig{Origins: origins}); err != nil {
		return nil, err
	}
	return httpServer, nil
}
