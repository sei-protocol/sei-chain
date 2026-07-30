package kv

import (
	"context"

	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	kvb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/block/kv"
	kvt "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/tx/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ indexer.EventSink = (*EventSink)(nil)

// The EventSink is an aggregator for redirecting the call path of the tx/block kvIndexer.
// For the implementation details please see the kv.go in the indexer/block and indexer/tx folder.
type EventSink struct {
	txi   *kvt.TxIndex
	bi    *kvb.BlockerIndexer
	store dbm.DB
}

// NewEventSink creates a KV event sink backed by store. budget, when non-nil,
// is a shared scan budget that bounds how many index entries all in-flight
// tx_search and block_search requests may visit at once; passing nil disables
// the cap. The same budget is shared by the tx and block indexers so the cap is
// process-wide across both.
func NewEventSink(store dbm.DB, budget *indexer.ScanBudget) indexer.EventSink {
	return &EventSink{
		txi:   kvt.NewTxIndex(store).WithScanBudget(budget),
		bi:    kvb.New(store).WithScanBudget(budget),
		store: store,
	}
}

func (kves *EventSink) Type() indexer.EventSinkType {
	return indexer.KV
}

func (kves *EventSink) IndexBlockEvents(bh types.EventDataNewBlockHeader) error {
	return kves.bi.Index(bh)
}

func (kves *EventSink) IndexTxEvents(results []*abci.TxResultV2) error {
	return kves.txi.Index(results)
}

func (kves *EventSink) SearchBlockEvents(ctx context.Context, q *query.Query, opts indexer.SearchOptions) ([]int64, error) {
	return kves.bi.Search(ctx, q, opts)
}

func (kves *EventSink) SearchTxEvents(ctx context.Context, q *query.Query, opts indexer.SearchOptions) ([]*abci.TxResultV2, error) {
	return kves.txi.Search(ctx, q, opts)
}

func (kves *EventSink) GetTxByHash(hash []byte) (*abci.TxResultV2, error) {
	return kves.txi.Get(hash)
}

func (kves *EventSink) HasBlock(h int64) (bool, error) {
	return kves.bi.Has(h)
}

func (kves *EventSink) Stop() error {
	return kves.store.Close()
}
