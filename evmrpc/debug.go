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

type DebugAPI struct {
	backend *DebugBackend
}

type DebugConfig struct {
	GasCap     uint64
	EVMTimeout time.Duration
}

type DebugBackend struct {
	*eth.EthAPIBackend
	ctxProvider func(int64) sdk.Context
	keeper      *keeper.Keeper
	tmClient    rpcclient.Client
	config      *DebugConfig
}

func NewDebugAPI(
	ctxProvider func(int64) sdk.Context,
	keeper *keeper.Keeper,
	tmClient rpcclient.Client,
	config *DebugConfig,
) *DebugAPI {
	return &DebugAPI{
		backend: NewDebugBackend(ctxProvider, keeper, tmClient, config),
	}
}

func NewDebugBackend(ctxProvider func(int64) sdk.Context, keeper *keeper.Keeper, tmClient rpcclient.Client, config *DebugConfig) *DebugBackend {
	return &DebugBackend{ctxProvider: ctxProvider, keeper: keeper, tmClient: tmClient, config: config}
}

func (a *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash) (result interface{}, returnErr error) {
	tracerAPI := tracers.NewAPI(a.backend)
	return tracerAPI.TraceTransaction(ctx, hash, nil)
}
