package abci

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (w KeeperWrapper) HandleBBNewBlock(sdkCtx sdk.Context, contractAddr string, epoch int64) error {
	msg := wasm.SudoNewBlockMsg{
		NewBlock: wasm.NewBlockRequest{Epoch: epoch},
	}
	_, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg, dexutils.ZeroUserProvidedGas)
	return err
}
