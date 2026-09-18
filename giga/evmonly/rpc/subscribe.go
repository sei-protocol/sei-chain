package rpc

import (
	"context"
	"errors"

	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// HeadSubscription yields the heights of committed blocks in order, one per
// block, without gaps. Next returns an error only when ctx ends.
type HeadSubscription interface {
	Next(context.Context) (int64, error)
}

// HeadSource publishes committed block heights.
type HeadSource interface {
	SubscribeNewHeads(context.Context) (HeadSubscription, error)
}

type subscribeAPI struct {
	backend Backend
	store   receipt.ReceiptStore
}

// NewHeads serves eth_subscribe("newHeads"), pushing one Ethereum header per
// committed block.
func (api *subscribeAPI) NewHeads(ctx context.Context) (*ethrpc.Subscription, error) {
	notifier, ok := ethrpc.NotifierFromContext(ctx)
	if !ok {
		return nil, ethrpc.ErrNotificationsUnsupported
	}
	heads, err := api.backend.SubscribeNewHeads(ctx)
	if err != nil {
		return nil, err
	}
	rpcSub := notifier.CreateSubscription()
	// The request ctx is canceled as soon as eth_subscribe returns; the stream
	// lives until rpcSub.Err() closes instead.
	go api.streamHeads(context.WithoutCancel(ctx), notifier, rpcSub, heads)
	return rpcSub, nil
}

func (api *subscribeAPI) streamHeads(ctx context.Context, notifier *ethrpc.Notifier, rpcSub *ethrpc.Subscription, heads HeadSubscription) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		// Err closes on eth_unsubscribe and when the connection drops; that is
		// the only way this stream ends.
		<-rpcSub.Err()
		cancel()
	}()
	for {
		height, err := heads.Next(ctx)
		if err != nil {
			return
		}
		header, err := api.header(ctx, height)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Error("newHeads: skipping block", "height", height, "err", err)
			continue
		}
		if err := notifier.Notify(rpcSub.ID, header); err != nil {
			return
		}
	}
}

// header renders the header of the block at height. Receipts are written
// asynchronously to block commit; gasUsed reads as zero if they are not yet
// readable.
func (api *subscribeAPI) header(ctx context.Context, height int64) (map[string]any, error) {
	h := coretypes.Int64(height)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &h})
	if err != nil {
		return nil, err
	}
	return encodeHeader(ctx, api.backend, api.store, block)
}
