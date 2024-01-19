package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"

	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

	evmAddr := types.MustGetEVMTransactionMessage(tx).Derived.SenderEVMAddr

	nextNonce := svd.evmKeeper.GetNonce(ctx, evmAddr)
	txNonce := ethTx.Nonce()

	// set EVM properties
	ctx = ctx.WithIsEVM(true)
	ctx = ctx.WithEVMNonce(txNonce)
	ctx = ctx.WithEVMSenderAddress(evmAddr.Hex())

	if ctx.IsCheckTx() {
		if txNonce < nextNonce {
			return ctx, sdkerrors.ErrWrongSequence
		}
		ctx = ctx.WithCheckTxCallback(func(e error) {
			if e != nil {
				return
			}
			txKey := tmtypes.Tx(ctx.TxBytes()).Key()
			svd.evmKeeper.AddPendingNonce(txKey, evmAddr, txNonce)
		})

		// if the mempool expires a transaction, this handler is invoked
		ctx = ctx.WithExpireTxHandler(func() {
			txKey := tmtypes.Tx(ctx.TxBytes()).Key()
			svd.evmKeeper.ExpirePendingNonce(txKey)
		})

		if txNonce > nextNonce {
			// transaction shall be added to mempool as a pending transaction
			ctx = ctx.WithPendingTxChecker(func() abci.PendingTxCheckerResponse {
				latestCtx := svd.latestCtxGetter()

				// nextNonceToBeMined is the next nonce that will be mined
				// geth calls SetNonce(n+1) after a transaction is mined
				nextNonceToBeMined := svd.evmKeeper.GetNonce(ctx, evmAddr)

				// nextPendingNonce is the minimum nonce a user may send without stomping on an already-sent
				// nonce, including non-mined or pending transactions
				// If a user skips a nonce [1,2,4], then this will be the value of that hole (e.g., 3)
				nextPendingNonce := svd.evmKeeper.CalculateNextNonce(latestCtx, evmAddr, true)

				if txNonce < nextNonceToBeMined {
					// this nonce has already been mined, we cannot accept it again
					return abci.Rejected
				} else if txNonce < nextPendingNonce {
					// this nonce is allowed to process as it is part of the
					// consecutive nonces from nextNonceToBeMined to nextPendingNonce
					// This logic allows multiple nonces from an account to be processed in a block.
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
