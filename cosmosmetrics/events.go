package cosmosmetrics

import (
	"context"
	"math/big"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// transferRecorder counts bond-denom bank transfers at or above a threshold.
type transferRecorder struct {
	count     metric.Int64Counter
	amount    metric.Float64Counter
	denom     atomic.Pointer[string]
	threshold *big.Int
}

func newTransferRecorder(count metric.Int64Counter, amount metric.Float64Counter, denom string, threshold uint64) *transferRecorder {
	r := &transferRecorder{count: count, amount: amount, threshold: new(big.Int).SetUint64(threshold)}
	r.setDenom(denom)
	return r
}

func (r *transferRecorder) setDenom(denom string) {
	r.denom.Store(&denom)
}

// ObserveTxResults counts every transfer event in a block's transaction results whose amount
// reaches the threshold. Only successful transactions are read.
func (r *transferRecorder) ObserveTxResults(ctx context.Context, results []*abci.ExecTxResult) {
	denom := *r.denom.Load()
	for _, res := range results {
		if res == nil || res.Code != 0 {
			continue
		}
		for i := range res.Events {
			r.observeEvent(ctx, &res.Events[i], denom)
		}
	}
}

func (r *transferRecorder) observeEvent(ctx context.Context, ev *abci.Event, denom string) {
	if ev.Type != banktypes.EventTypeTransfer {
		return
	}
	var amount string
	for _, attr := range ev.Attributes {
		if string(attr.Key) == sdk.AttributeKeyAmount {
			amount = string(attr.Value)
		}
	}
	coin, ok := denomCoin(amount, denom)
	if !ok || coin.Amount.BigInt().Cmp(r.threshold) < 0 {
		return
	}
	value, _ := new(big.Float).SetInt(coin.Amount.BigInt()).Float64()
	attrs := metric.WithAttributes(denomAttr(denom))
	r.count.Add(ctx, 1, attrs)
	r.amount.Add(ctx, value, attrs)
}

// denomCoin parses only the denom's entry out of a comma-separated coins string, skipping the
// parse entirely when the denom cannot be present.
func denomCoin(coins, denom string) (sdk.Coin, bool) {
	if !strings.Contains(coins, denom) {
		return sdk.Coin{}, false
	}
	for s := range strings.SplitSeq(coins, ",") {
		if !strings.HasSuffix(s, denom) {
			continue
		}
		coin, err := sdk.ParseCoinNormalized(s)
		if err != nil || coin.Denom != denom {
			continue
		}
		return coin, true
	}
	return sdk.Coin{}, false
}

// ObserveTxResults counts the bank transfers in a finalized block's transaction results.
func (r *Reporter) ObserveTxResults(ctx context.Context, results []*abci.ExecTxResult) {
	r.transfers.ObserveTxResults(ctx, results)
}
