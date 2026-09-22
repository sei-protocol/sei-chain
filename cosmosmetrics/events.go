package cosmosmetrics

import (
	"context"
	"math/big"

	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// transferRecorder records bond-denom bank transfers at or above a threshold.
type transferRecorder struct {
	gauge     metric.Float64Gauge
	denom     string
	threshold *big.Int
}

func newTransferRecorder(gauge metric.Float64Gauge, denom string, threshold uint64) *transferRecorder {
	return &transferRecorder{gauge: gauge, denom: denom, threshold: new(big.Int).SetUint64(threshold)}
}

// ObserveTxResults records every transfer event in a block's transaction results whose amount
// reaches the threshold. Only successful transactions are read.
func (r *transferRecorder) ObserveTxResults(ctx context.Context, results []*abci.ExecTxResult) {
	for _, res := range results {
		if res == nil || res.Code != 0 {
			continue
		}
		for i := range res.Events {
			r.observeEvent(ctx, &res.Events[i])
		}
	}
}

func (r *transferRecorder) observeEvent(ctx context.Context, ev *abci.Event) {
	if ev.Type != banktypes.EventTypeTransfer {
		return
	}
	var amount string
	for _, attr := range ev.Attributes {
		if string(attr.Key) == sdk.AttributeKeyAmount {
			amount = string(attr.Value)
		}
	}
	coins, err := sdk.ParseCoinsNormalized(amount)
	if err != nil {
		return
	}
	amt := coins.AmountOf(r.denom)
	if amt.BigInt().Cmp(r.threshold) < 0 {
		return
	}
	value, _ := new(big.Float).SetInt(amt.BigInt()).Float64()
	r.gauge.Record(ctx, value, metric.WithAttributes(denomAttr(r.denom)))
}

// ObserveTxResults records the bank transfers in a finalized block's transaction results.
func (r *Reporter) ObserveTxResults(ctx context.Context, results []*abci.ExecTxResult) {
	r.transfers.ObserveTxResults(ctx, results)
}
