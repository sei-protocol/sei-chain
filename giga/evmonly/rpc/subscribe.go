package rpc

import (
	"context"
	"errors"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// receiptWait bounds how long a newHeads notification waits for a block's
// receipts to become readable before it is sent with whatever gasUsed the
// receipt store can answer.
const receiptWait = 2 * time.Second

// HeadSubscription yields the heights of newly committed blocks in order.
// Next returns an error once the subscription is canceled or the subscriber
// has fallen too far behind.
type HeadSubscription interface {
	Next(context.Context) (int64, error)
	Cancel()
}

// HeadSource publishes newly committed block heights.
type HeadSource interface {
	SubscribeNewHeads(ctx context.Context, clientID string) (HeadSubscription, error)
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
	rpcSub := notifier.CreateSubscription()
	heads, err := api.backend.SubscribeNewHeads(ctx, string(rpcSub.ID))
	if err != nil {
		return nil, err
	}
	go api.streamHeads(notifier, rpcSub, heads)
	return rpcSub, nil
}

func (api *subscribeAPI) streamHeads(notifier *ethrpc.Notifier, rpcSub *ethrpc.Subscription, heads HeadSubscription) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer heads.Cancel()
	go func() {
		// Err closes on eth_unsubscribe and when the connection drops.
		<-rpcSub.Err()
		cancel()
	}()
	for {
		height, err := heads.Next(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Debug("newHeads subscription ended", "id", rpcSub.ID, "err", err)
			}
			return
		}
		header, err := api.header(ctx, height)
		if err != nil {
			logger.Error("newHeads header lookup failed", "height", height, "err", err)
			return
		}
		if err := notifier.Notify(rpcSub.ID, header); err != nil {
			return
		}
	}
}

// header renders the header of the block at height once its receipts are
// readable, or after receiptWait elapses.
func (api *subscribeAPI) header(ctx context.Context, height int64) (map[string]any, error) {
	h := coretypes.Int64(height)
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &h})
	if err != nil {
		return nil, err
	}
	awaitReceipts(ctx, api.store, height)
	return encodeHeader(ctx, api.backend, api.store, block)
}

// awaitReceipts blocks until store.LatestVersion reaches height, ctx ends, or
// receiptWait elapses. Receipts are written asynchronously to block commit and
// may briefly trail the head.
func awaitReceipts(ctx context.Context, store receipt.ReceiptStore, height int64) {
	if store.LatestVersion() >= height {
		return
	}
	deadline := time.NewTimer(receiptWait)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
			if store.LatestVersion() >= height {
				return
			}
		}
	}
}
