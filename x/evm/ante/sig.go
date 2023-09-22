package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMSigVerifyDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func (svd EVMSigVerifyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	ethTx, found := evmtypes.GetContextEthTx(ctx)
	if !found {
		return ctx, errors.New("EVM transaction is not found in EVM ante route")
	}
	if !ethTx.Protected() {
		return ctx, errors.New("EVM transaction is not replay protected")
	}

	_, found = evmtypes.GetContextEVMAddress(ctx)
	if !found {
		return ctx, errors.New("failed to get sender from EVM tx")
	}

	return ctx, nil
}
