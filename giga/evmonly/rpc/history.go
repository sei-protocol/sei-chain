package rpc

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// History serves finalized chain data the node's own stores no longer hold.
type History interface {
	TransactionByHash(ctx context.Context, hash common.Hash) (archive.Transaction, error)
}

// errHistoryUnavailable is the error a client sees when history could not answer.
var errHistoryUnavailable = errors.New("history archive unavailable")

var historyReads = utils.OrPanic1(otel.Meter("giga_evmonly_rpc").Int64Counter(
	"giga_evmonly_rpc_history_reads_total",
	metric.WithDescription("EVM-only RPC reads answered from history, by method and outcome (found, not_found, error)"),
	metric.WithUnit("{read}"),
))

// historyTransactionByHash returns hash's transaction from history, or nil when history holds none
// or none is configured.
func (api *txAPI) historyTransactionByHash(ctx context.Context, hash common.Hash) (*export.RPCTransaction, error) {
	history, ok := api.history.Get()
	if !ok {
		return nil, nil
	}
	tx, err := history.TransactionByHash(ctx, hash)
	outcome := "found"
	defer func() {
		historyReads.Add(ctx, 1, metric.WithAttributes(
			attribute.String("method", "eth_getTransactionByHash"), attribute.String("outcome", outcome)))
	}()
	switch {
	case err == nil:
		return encodeHistoryTransaction(tx, api.backend.EvmChainConfig()), nil
	case errors.Is(err, archive.ErrNotFound):
		outcome = "not_found"
		return nil, nil
	case ctx.Err() != nil:
		outcome = "error"
		return nil, ctx.Err()
	default:
		outcome = "error"
		// Only the log names the cause: a null result would claim the transaction does not exist,
		// and the cause names the archive's internals.
		logger.Error("history read failed", "method", "eth_getTransactionByHash", "hash", hash, "err", err)
		return nil, errHistoryUnavailable
	}
}

// encodeHistoryTransaction returns tx as eth_getTransactionByHash returns a transaction the node
// holds.
func encodeHistoryTransaction(tx archive.Transaction, chainConfig *params.ChainConfig) *export.RPCTransaction {
	result := export.NewRPCTransaction(tx.Tx, tx.Block.Hash, tx.Block.Number, tx.Block.Time,
		uint64(tx.Index), tx.Block.BaseFee, chainConfig)
	if result.From == (common.Address{}) {
		result.From = tx.Sender
	}
	return result
}
