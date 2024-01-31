package evmrpc

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"time"
)

type TraceAPI struct {
	backend *TraceBackend
}

type TraceConfig struct {
	GasCap     uint64
	EVMTimeout time.Duration
}

type TraceBackend struct {
	*eth.EthAPIBackend
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *TraceConfig
}

func NewTraceAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	tmClient rpcclient.Client,
	config *TraceConfig,
) *TraceAPI {
	return &TraceAPI{
		backend: NewTraceBackend(ctxProvider, keeper, tmClient, config),
	}
}

func NewTraceBackend(ctxProvider func(int64) sdk.Context, keeper *keeper.Keeper, tmClient rpcclient.Client, config *TraceConfig) *TraceBackend {
	return &TraceBackend{ctxProvider: ctxProvider, keeper: keeper, tmClient: tmClient, config: config}
}

func (a *TraceAPI) TraceTransaction(ctx context.Context, hash common.Hash) (result interface{}, returnErr error) {
	tracerAPI := tracers.NewAPI(a.backend)
	return tracerAPI.TraceTransaction(ctx, hash, nil)
}
