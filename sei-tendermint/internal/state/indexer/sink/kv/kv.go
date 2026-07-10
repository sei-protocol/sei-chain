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

func NewEventSink(store dbm.DB) indexer.EventSink {
	return &EventSink{
		txi:   kvt.NewTxIndex(store),
		bi:    kvb.New(store),
		store: store,
	}
}

// NewEventSinkSkipWatermark builds a KV event sink whose indexers dual-write the
// height-ordered index but never touch the height-ordered index watermark.
func NewEventSinkSkipWatermark(store dbm.DB) indexer.EventSink {
	return &EventSink{
		txi:   kvt.NewTxIndexSkipWatermark(store),
		bi:    kvb.NewSkipWatermark(store),
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

// BlockWatermark returns the block height-ordered index watermark and whether it has been anchored.
func (kves *EventSink) BlockWatermark() (height int64, set bool, err error) {
	return kves.bi.Watermark()
}

// TxWatermark returns the tx height-ordered index watermark and whether it has been anchored.
func (kves *EventSink) TxWatermark() (height int64, set bool, err error) {
	return kves.txi.Watermark()
}

func (kves *EventSink) Stop() error {
	return kves.store.Close()
}
