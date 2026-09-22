package cosmosmetrics

import (
	"context"
	"math/big"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// transferRecorder records bank transfers at or above a threshold on a gauge keyed by
// denom, sender and recipient.
type transferRecorder struct {
	gauge     metric.Float64Gauge
	threshold *big.Int
}

func newTransferRecorder(gauge metric.Float64Gauge, threshold uint64) *transferRecorder {
	return &transferRecorder{gauge: gauge, threshold: new(big.Int).SetUint64(threshold)}
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
	var amount, sender, recipient string
	for _, attr := range ev.Attributes {
		switch string(attr.Key) {
		case sdk.AttributeKeyAmount:
			amount = string(attr.Value)
		case banktypes.AttributeKeySender:
			sender = string(attr.Value)
		case banktypes.AttributeKeyRecipient:
			recipient = string(attr.Value)
		}
	}
	coins, err := sdk.ParseCoinsNormalized(amount)
	if err != nil {
		return
	}
	for _, coin := range coins {
		if coin.Amount.BigInt().Cmp(r.threshold) < 0 {
			continue
		}
		value, _ := new(big.Float).SetInt(coin.Amount.BigInt()).Float64()
		r.gauge.Record(ctx, value, metric.WithAttributes(
			denomAttr(coin.Denom),
			attribute.String("sender", sender),
			attribute.String("recipient", recipient),
		))
	}
}

// ObserveTxResults records the bank transfers in a finalized block's transaction results.
func (c *Collector) ObserveTxResults(ctx context.Context, results []*abci.ExecTxResult) {
	c.transfers.ObserveTxResults(ctx, results)
}
