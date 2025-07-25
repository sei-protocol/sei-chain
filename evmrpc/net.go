package evmrpc

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/tendermint/tendermint/libs/time"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
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
	defer recordMetrics("net_version", i.connectionType, startTime, true)
	return fmt.Sprintf("%d", i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)).Uint64())
}
