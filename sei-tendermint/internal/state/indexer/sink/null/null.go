package null

import (
	"context"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ indexer.EventSink = (*EventSink)(nil)

// EventSink implements a no-op indexer.
type EventSink struct{}

func NewEventSink() indexer.EventSink {
	return &EventSink{}
}

func (nes *EventSink) Type() indexer.EventSinkType {
	return indexer.NULL
}

func (nes *EventSink) IndexBlockEvents(bh types.EventDataNewBlockHeader) error {
	return nil
}

func (nes *EventSink) IndexTxEvents(results []*abci.TxResultV2) error {
	return nil
}

func (nes *EventSink) SearchBlockEvents(ctx context.Context, q *query.Query) ([]int64, error) {
	return nil, nil
}

func (nes *EventSink) SearchTxEvents(ctx context.Context, q *query.Query) ([]*abci.TxResultV2, error) {
	return nil, nil
}

func (nes *EventSink) GetTxByHash(hash []byte) (*abci.TxResultV2, error) {
	return nil, nil
}

func (nes *EventSink) HasBlock(h int64) (bool, error) {
	return false, nil
}

func (nes *EventSink) Stop() error {
	return nil
}
