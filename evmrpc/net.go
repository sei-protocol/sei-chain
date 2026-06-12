package evmrpc

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type NetAPI struct {
	tmClient       client.LocalClient
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
}

func NewNetAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, connectionType ConnectionType) *NetAPI {
	return &NetAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, connectionType: connectionType}
}

func (i *NetAPI) Version(ctx context.Context) string {
	startTime := time.Now()
	defer recordMetrics(ctx, "net_version", i.connectionType, startTime)
	return fmt.Sprintf("%d", i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)).Uint64())
}
