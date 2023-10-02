package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMSigVerifyDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{evmKeeper: evmKeeper}
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

	seiAddr, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return ctx, errors.New("failed to get sender sei address from EVM tx")
	}
	acc := svd.evmKeeper.AccountKeeper().GetAccount(ctx, seiAddr)
	if acc.GetSequence() != ethTx.Nonce() {
		return ctx, sdkerrors.ErrWrongSequence
	}
	if err := acc.SetSequence(ethTx.Nonce() + 1); err != nil {
		return ctx, err
	}
	svd.evmKeeper.AccountKeeper().SetAccount(ctx, acc)

	return ctx, nil
}
