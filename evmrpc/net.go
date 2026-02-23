package evmrpc

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type NetAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
}

func NewNetAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, connectionType ConnectionType) *NetAPI {
	return &NetAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, connectionType: connectionType}
}

func (i *NetAPI) Version() string {
	startTime := time.Now()
	defer recordMetrics("net_version", i.connectionType, startTime)
	return fmt.Sprintf("%d", i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)).Uint64())
}
