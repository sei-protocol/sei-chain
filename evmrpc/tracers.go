package evmrpc

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // run init()s to register native tracers
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type DebugAPI struct {
	tracersAPI     *tracers.API
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txDecoder      sdk.TxDecoder
	connectionType ConnectionType
}

func NewDebugAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, config *SimulateConfig, connectionType ConnectionType) *DebugAPI {
	backend := NewBackend(ctxProvider, k, txDecoder, tmClient, config)
	tracersAPI := tracers.NewAPI(backend)
	return &DebugAPI{tracersAPI: tracersAPI, tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, connectionType: connectionType}
}

func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (interface{}, error) {
	startTime := time.Now()
	defer recordMetrics("debug_traceTransaction", api.connectionType, startTime, true)
	return api.tracersAPI.TraceTransaction(ctx, hash, config)
}

func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (interface{}, error) {
	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByNumber", api.connectionType, startTime, true)
	return api.tracersAPI.TraceBlockByNumber(ctx, number, config)
}

func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (interface{}, error) {
	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByHash", api.connectionType, startTime, true)
	return api.tracersAPI.TraceBlockByHash(ctx, hash, config)
}
