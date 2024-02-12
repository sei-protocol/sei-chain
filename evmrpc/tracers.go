package evmrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type DebugAPI struct {
	tracersAPI  *tracers.API
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewDebugAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, config *SimulateConfig) *DebugAPI {
	backend := NewBackend(ctxProvider, k, txDecoder, tmClient, config)
	tracersAPI := tracers.NewAPI(backend)
	return &DebugAPI{tracersAPI: tracersAPI, tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

// TODO: can potentially just extend tracersAPI
func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (interface{}, error) {
	fmt.Printf("In TraceTransaction, requesting tx %s\n", hash.Hex())
	startTime := time.Now()
	defer recordMetrics("debug_traceTransaction", startTime, true)
	fmt.Printf("In TraceTransaction, config = %v\n", config)
	res, err := api.tracersAPI.TraceTransaction(ctx, hash, config)
	fmt.Printf("In TraceTransaction, res = %v, err = %v\n", res, err)
	var r json.RawMessage
	resBytes := res.(json.RawMessage)
	err = r.UnmarshalJSON(resBytes)
	if err != nil {
		panic(err)
	}
	fmt.Println("In TraceTransaction, res as string = ", string(resBytes))
	return res, nil
}
