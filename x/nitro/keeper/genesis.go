package keeper

import (
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	if genState.Params.WhitelistedTxSenders == nil {
		genState.Params.WhitelistedTxSenders = []string{}
	}
	k.SetParams(ctx, genState.Params)

	if genState.Sender != "" {
		k.SetSender(ctx, genState.Slot, genState.Sender)
		root, err := hex.DecodeString(genState.StateRoot)
		if err != nil {
			panic(err)
		}
		k.SetStateRoot(ctx, genState.Slot, root)
		txs := [][]byte{}
		for _, tx := range genState.Txs {
			txbz, err := hex.DecodeString(tx)
			if err != nil {
				panic(err)
			}
			txs = append(txs, txbz)
		}
		k.SetTransactionData(ctx, genState.Slot, txs)
	}
}

func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		Params: k.GetParams(ctx),
	}
}
