package cosmosmetrics

import (
	"math/big"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// transferTTL is how long a reported transfer stays on the endpoint.
const transferTTL = 5 * time.Minute

// transferGauge reports bank transfers at or above a threshold for a bounded time.
type transferGauge struct {
	gauge     *prometheus.GaugeVec
	threshold *big.Int
	ttl       time.Duration

	mu     sync.Mutex
	timers map[transferKey]*time.Timer
	gen    map[transferKey]uint64
}

type transferKey struct {
	denom, sender, recipient string
}

func newTransferGauge(threshold uint64, ttl time.Duration) *transferGauge {
	return &transferGauge{
		gauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cosmos_bank_transfer_amount",
			Help: "Number of tokens transferred in a transfer message",
		}, []string{"denom", "sender", "recipient"}),
		threshold: new(big.Int).SetUint64(threshold),
		ttl:       ttl,
		timers:    map[transferKey]*time.Timer{},
		gen:       map[transferKey]uint64{},
	}
}

// ObserveTxResults records every transfer event in a block's transaction results whose amount
// reaches the threshold. Only successful transactions are read.
func (g *transferGauge) ObserveTxResults(results []*abci.ExecTxResult) {
	for _, res := range results {
		if res == nil || res.Code != 0 {
			continue
		}
		for i := range res.Events {
			g.observeEvent(&res.Events[i])
		}
	}
}

func (g *transferGauge) observeEvent(ev *abci.Event) {
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
		if coin.Amount.BigInt().Cmp(g.threshold) < 0 {
			continue
		}
		value, _ := new(big.Float).SetInt(coin.Amount.BigInt()).Float64()
		g.set(transferKey{denom: coin.Denom, sender: sender, recipient: recipient}, value)
	}
}

func (g *transferGauge) set(key transferKey, value float64) {
	labels := prometheus.Labels{"denom": key.denom, "sender": key.sender, "recipient": key.recipient}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gauge.With(labels).Set(value)
	if t, ok := g.timers[key]; ok {
		t.Stop()
	}
	gen := g.gen[key] + 1
	g.gen[key] = gen
	g.timers[key] = time.AfterFunc(g.ttl, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.gen[key] != gen {
			return
		}
		g.gauge.Delete(labels)
		delete(g.timers, key)
		delete(g.gen, key)
	})
}

// ObserveTxResults records the bank transfers in a finalized block's transaction results.
func (c *Collector) ObserveTxResults(results []*abci.ExecTxResult) {
	c.transfers.ObserveTxResults(results)
}
