package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

type EVMSigVerifyDecorator struct {
	evmKeeper       *evmkeeper.Keeper
	latestCtxGetter func() sdk.Context // should be read-only
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper, latestCtxGetter func() sdk.Context) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{
		evmKeeper:       evmKeeper,
		latestCtxGetter: latestCtxGetter,
	}
}

func (svd *EVMSigVerifyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	ethTx, _ := types.MustGetEVMTransactionMessage(tx).AsTransaction()

	if !ethTx.Protected() {
		return ctx, sdkerrors.ErrNoSignatures
	}

	evmAddr := common.BytesToAddress(types.MustGetEVMTransactionMessage(tx).Derived.SenderEVMAddr)

	nextNonce := svd.evmKeeper.GetNonce(ctx, evmAddr)
	txNonce := ethTx.Nonce()

	if ctx.IsCheckTx() {
		if txNonce < nextNonce {
			return ctx, sdkerrors.ErrWrongSequence
		} else if txNonce > nextNonce {
			// transaction shall be added to mempool as a pending transaction
			ctx = ctx.WithPendingTxChecker(func() abci.PendingTxCheckerResponse {
				latestNonce := svd.evmKeeper.GetNonce(svd.latestCtxGetter(), evmAddr)
				if txNonce < latestNonce {
					return abci.Rejected
				} else if txNonce == latestNonce {
					return abci.Accepted
				}
				return abci.Pending
			})
		}
	} else if txNonce != nextNonce {
		return ctx, sdkerrors.ErrWrongSequence
	}

	return next(ctx, tx, simulate)
}
