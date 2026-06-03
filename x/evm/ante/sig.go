package ante

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/seilog"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	utilmetrics "github.com/sei-protocol/sei-chain/utils/metrics"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var logger = seilog.NewLogger("x", "evm", "ante")

type EVMSigVerifyDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewEVMSigVerifyDecorator(evmKeeper *evmkeeper.Keeper, _ func() sdk.Context) *EVMSigVerifyDecorator {
	return &EVMSigVerifyDecorator{
		evmKeeper: evmKeeper,
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
	ctx = ctx.WithEVMSenderAddress(evmAddr)
	ctx = ctx.WithSeiSenderAddress(types.MustGetEVMTransactionMessage(tx).Derived.SenderSeiAddr)
	ctx = ctx.WithEVMTxHash(ethTx.Hash())

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
			logger.Debug("chainID mismatch", "txChainID", ethTx.ChainId(), "chainID", chainID)
			return ctx, sdkerrors.ErrInvalidChainID
		}
	default:
		// after legacy, all transactions must have the correct chain ID
		if txChainID.Cmp(chainID) != 0 {
			logger.Debug("chainID mismatch", "txChainID", ethTx.ChainId(), "chainID", chainID)
			return ctx, sdkerrors.ErrInvalidChainID
		}
	}

	if ctx.IsCheckTx() {
		if txNonce < nextNonce {
			return ctx, sdkerrors.ErrWrongSequence
		}
		ctx = ctx.WithEVMRequiredBalance(fee)
	} else if txNonce != nextNonce {
		tooHigh := txNonce > nextNonce
		utilmetrics.IncrementNonceMismatch(tooHigh) // TODO(PLT-330): remove once evm_nonce_mismatch_total verified
		evmAnteMetrics.nonceMismatch.Add(ctx.Context(), 1, otelmetric.WithAttributes(attribute.Bool("too_high", tooHigh)))
		return ctx, sdkerrors.ErrWrongSequence
	}

	return next(ctx, tx, simulate)
}
