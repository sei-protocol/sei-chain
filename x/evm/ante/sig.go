package ante

import (
	"encoding/binary"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMSigVerifyDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{evmKeeper: evmKeeper}
}

func (svd EVMSigVerifyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	ethTx, found := types.GetContextEthTx(ctx)
	if !found {
		return ctx, errors.New("EVM transaction is not found in EVM ante route")
	}
	if !ethTx.Protected() {
		return ctx, errors.New("EVM transaction is not replay protected")
	}

	evmAddr, found := types.GetContextEVMAddress(ctx)
	if !found {
		return ctx, errors.New("failed to get sender from EVM tx")
	}

	lastNonce := uint64(0)
	noncebz := svd.evmKeeper.PrefixStore(ctx, types.NonceKeyPrefix).Get(evmAddr[:])
	if noncebz != nil {
		lastNonce = binary.BigEndian.Uint64(noncebz)
	}

	if ethTx.Nonce() == lastNonce {
		return ctx, sdkerrors.ErrWrongSequence
	}

	return next(ctx, tx, simulate)
}
