package ante

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/utils/metrics"
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

	evmAddr := types.MustGetEVMTransactionMessage(tx).Derived.SenderEVMAddr

	nextNonce := svd.evmKeeper.GetNonce(ctx, evmAddr)
	txNonce := ethTx.Nonce()

	// set EVM properties
	ctx = ctx.WithIsEVM(true)
	ctx = ctx.WithEVMNonce(txNonce)
	ctx = ctx.WithEVMSenderAddress(evmAddr.Hex())
	ctx = ctx.WithEVMTxHash(ethTx.Hash().Hex())

	chainID := svd.evmKeeper.ChainID(ctx)
	txChainID := ethTx.ChainId()

	fee := new(big.Int).Mul(ethTx.GasPrice(), new(big.Int).SetUint64(ethTx.Gas()))
	if ethTx.Value() != nil {
		fee = new(big.Int).Add(fee, ethTx.Value())
	}

	// validate chain ID on the transaction
	switch ethTx.Type() {
	case ethtypes.LegacyTxType:
		// legacy either can have a zero or correct chain ID
		if txChainID.Cmp(big.NewInt(0)) != 0 && txChainID.Cmp(chainID) != 0 {
			ctx.Logger().Debug("chainID mismatch", "txChainID", ethTx.ChainId(), "chainID", chainID)
			return ctx, sdkerrors.ErrInvalidChainID
		}
	default:
		// after legacy, all transactions must have the correct chain ID
		if txChainID.Cmp(chainID) != 0 {
			ctx.Logger().Debug("chainID mismatch", "txChainID", ethTx.ChainId(), "chainID", chainID)
			return ctx, sdkerrors.ErrInvalidChainID
		}
	}

	if ctx.IsCheckTx() {
		if txNonce < nextNonce {
			return ctx, sdkerrors.ErrWrongSequence
		}
		ctx = ctx.WithCheckTxCallback(func(priority int64) {
			txKey := tmtypes.Tx(ctx.TxBytes()).Key()
			svd.evmKeeper.AddPendingNonce(txKey, evmAddr, txNonce, priority)
			metrics.IncrementPendingNonce("added")
		})

		// if the mempool expires a transaction, this handler is invoked
		ctx = ctx.WithExpireTxHandler(func() {
			txKey := tmtypes.Tx(ctx.TxBytes()).Key()
			svd.evmKeeper.RemovePendingNonce(txKey)
			metrics.IncrementPendingNonce("expired")
		})

		if txNonce > nextNonce {
			// transaction shall be added to mempool as a pending transaction
			ctx = ctx.WithPendingTxChecker(func() abci.PendingTxCheckerResponse {
				latestCtx := svd.latestCtxGetter()

				// nextNonceToBeMined is the next nonce that will be mined
				// geth calls SetNonce(n+1) after a transaction is mined
				nextNonceToBeMined := svd.evmKeeper.GetNonce(latestCtx, evmAddr)

				// nextPendingNonce is the minimum nonce a user may send without stomping on an already-sent
				// nonce, including non-mined or pending transactions
				// If a user skips a nonce [1,2,4], then this will be the value of that hole (e.g., 3)
				nextPendingNonce := svd.evmKeeper.CalculateNextNonce(latestCtx, evmAddr, true)

				if txNonce < nextNonceToBeMined {
					// this nonce has already been mined, we cannot accept it again
					metrics.IncrementPendingNonce("rejected")
					return abci.Rejected
				} else if txNonce < nextPendingNonce {
					// check if the sender still has enough funds to pay for gas
					balance := svd.evmKeeper.GetBalance(latestCtx, types.MustGetEVMTransactionMessage(tx).Derived.SenderSeiAddr)
					if balance.Cmp(fee) < 0 {
						// not enough funds. Go back to pending as it may obtain sufficient funds later.
						return abci.Pending
					}
					// this nonce is allowed to process as it is part of the
					// consecutive nonces from nextNonceToBeMined to nextPendingNonce
					// This logic allows multiple nonces from an account to be processed in a block.
					metrics.IncrementPendingNonce("accepted")
					return abci.Accepted
				}
				return abci.Pending
			})
		}
	} else if txNonce != nextNonce {
		metrics.IncrementNonceMismatch(txNonce > nextNonce)
		return ctx, sdkerrors.ErrWrongSequence
	}

	return next(ctx, tx, simulate)
}
