package evmrpc

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type NetAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewNetAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder) *NetAPI {
	return &NetAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

func (i *NetAPI) Version() string {
	return fmt.Sprintf("%d", i.keeper.ChainID(i.ctxProvider(LatestCtxHeight)).Uint64())
}
