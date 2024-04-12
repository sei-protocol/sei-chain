package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type AnteErrorHandler struct {
	wrapped sdk.AnteHandler
	k       *keeper.Keeper
}

func NewAnteErrorHandler(wrapped sdk.AnteHandler, k *keeper.Keeper) *AnteErrorHandler {
	return &AnteErrorHandler{wrapped: wrapped, k: k}
}

// if there is any error in ante handler, record it in deferred info so that a receipt
// can be written for it in the EndBlock. (we can't directly write receipt here since
// we still need to return an error which will cause any write here to revert)
func (a *AnteErrorHandler) Handle(ctx sdk.Context, tx sdk.Tx, simulate bool) (newCtx sdk.Context, err error) {
	newCtx, err = a.wrapped(ctx, tx, simulate)
	if err != nil && !ctx.IsCheckTx() && !ctx.IsReCheckTx() && !simulate {
		msg := types.MustGetEVMTransactionMessage(tx)
		txData, unpackerr := types.UnpackTxData(msg.Data)
		if unpackerr != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to unpack message data %X", msg.Data.Value))
			return
		}
		if _, ok := txData.(*ethtx.AssociateTx); ok {
			return
		}
		a.k.AppendErrorToEvmTxDeferredInfo(ctx, ethtypes.NewTx(txData.AsEthereumData()).Hash(), err.Error())
	}
	return
}
