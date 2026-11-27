package evmonly

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ethereum/go-ethereum/core"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var rejectedTxLogger = seilog.NewLogger("giga", "evmonly", "rejected")

// The reasons a block rejects a transaction, the label vocabulary of giga_evmonly_txs_rejected_total.
const (
	txRejectionGasLimitReached   = "gas_limit_reached"
	txRejectionNonceTooHigh      = "nonce_too_high"
	txRejectionNonceTooLow       = "nonce_too_low"
	txRejectionInsufficientFunds = "insufficient_funds"
	txRejectionOther             = "other"
)

var txRejectionReasons = []string{
	txRejectionGasLimitReached,
	txRejectionNonceTooHigh,
	txRejectionNonceTooLow,
	txRejectionInsufficientFunds,
	txRejectionOther,
}

var txRejectionReasonOptions = txRejectionReasonOptionTable()

func txRejectionReasonOptionTable() map[string]metric.MeasurementOption {
	table := make(map[string]metric.MeasurementOption, len(txRejectionReasons))
	for _, reason := range txRejectionReasons {
		table[reason] = metric.WithAttributes(attribute.String("reason", reason))
	}
	return table
}

// txRejectionReason maps the error a rejected transaction carries onto a rejection reason.
func txRejectionReason(err error) string {
	switch {
	case errors.Is(err, core.ErrGasLimitReached):
		return txRejectionGasLimitReached
	case errors.Is(err, core.ErrNonceTooHigh):
		return txRejectionNonceTooHigh
	case errors.Is(err, core.ErrNonceTooLow):
		return txRejectionNonceTooLow
	case errors.Is(err, core.ErrInsufficientFunds), errors.Is(err, core.ErrInsufficientFundsForTransfer):
		return txRejectionInsufficientFunds
	default:
		return txRejectionOther
	}
}

// logRejectedTxs logs one summary line for a block that rejected any transactions, and each
// rejected transaction at debug level. txs are the block's transactions, in the order of
// result.Txs.
func logRejectedTxs(ctx context.Context, block BlockContext, txs []PreparedTx, result *BlockResult) {
	debug := rejectedTxLogger.Enabled(ctx, slog.LevelDebug)
	rejections := make(map[string]int, len(txRejectionReasons))
	rejected := 0
	for i, tx := range result.Txs {
		if !tx.Rejected {
			continue
		}
		reason := txRejectionReason(tx.Err)
		rejections[reason]++
		rejected++
		if debug && i < len(txs) {
			rejectedTxLogger.DebugContext(ctx, "block rejected transaction",
				"height", block.Number,
				"hash", tx.Hash,
				"sender", tx.Sender,
				"nonce", txs[i].Tx.Nonce(),
				"gas_limit", txs[i].Tx.Gas(),
				"reason", reason,
				"err", tx.Err,
			)
		}
	}
	if rejected == 0 {
		return
	}
	attrs := []any{
		"height", block.Number,
		"txs", len(result.Txs),
		"rejected", rejected,
		"gas_used", result.GasUsed,
		"gas_limit", block.GasLimit,
	}
	for _, reason := range txRejectionReasons {
		if n := rejections[reason]; n > 0 {
			attrs = append(attrs, reason, n)
		}
	}
	rejectedTxLogger.InfoContext(ctx, "block rejected transactions", attrs...)
}
