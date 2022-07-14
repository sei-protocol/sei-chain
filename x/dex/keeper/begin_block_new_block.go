package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k *Keeper) HandleBBNewBlock(sdkCtx sdk.Context, contractAddr string, epoch int64) error {
	msg := types.SudoNewBlockMsg{
		NewBlock: types.NewBlockRequest{Epoch: epoch},
	}
	_, err := k.CallContractSudo(sdkCtx, contractAddr, msg)
	return err
}
