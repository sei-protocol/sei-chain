package rpc

import (
	"context"
	"errors"
	"fmt"

	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type subscribeAPI struct {
	backend Backend
	store   receipt.ReceiptStore
}

// NewHeads serves eth_subscribe("newHeads"), pushing one Ethereum header per
// block executed after the subscription is created.
func (api *subscribeAPI) NewHeads(ctx context.Context) (*ethrpc.Subscription, error) {
	notifier, ok := ethrpc.NotifierFromContext(ctx)
	if !ok {
		return nil, ethrpc.ErrNotificationsUnsupported
	}
	executed, err := api.backend.ExecutedBlocks()
	if err != nil {
		return nil, err
	}
	rpcSub := notifier.CreateSubscription()
	// The cursor is taken before the stream goroutine starts so a block committed
	// in between is not skipped.
	last := executed.Load().Latest().Number
	// The request ctx is canceled as soon as eth_subscribe returns; the stream
	// lives until rpcSub.Err() closes instead.
	go api.streamHeads(context.WithoutCancel(ctx), notifier, rpcSub, executed, last)
	return rpcSub, nil
}

func (api *subscribeAPI) streamHeads(
	ctx context.Context,
	notifier *ethrpc.Notifier,
	rpcSub *ethrpc.Subscription,
	executed utils.AtomicRecv[atypes.ExecutedBlocks],
	last atypes.GlobalBlockNumber,
) {
	// Unlike request handlers, this goroutine is outside go-ethereum's per-call
	// recovery, so a panic here must end the subscription rather than the node.
	defer func() {
		if r := recover(); r != nil {
			logger.Error("newHeads: stream panicked", "subscription", rpcSub.ID, "panic", r)
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		// Err closes on eth_unsubscribe and when the connection drops; that is
		// the only way this stream ends.
		<-rpcSub.Err()
		cancel()
	}()
	for next := last + 1; ; next++ {
		window, err := executed.Wait(ctx, func(w atypes.ExecutedBlocks) bool { return w.Latest().Number >= next })
		if err != nil {
			return
		}
		header, err := api.header(ctx, next, window)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Error("newHeads: skipping block", "height", next, "err", err)
			continue
		}
		if err := notifier.Notify(rpcSub.ID, header); err != nil {
			return
		}
	}
}

// header renders the header of the block at height. gasUsed is the total the
// commit recorded while window still holds height; for an older height the
// receipt store is read instead, yielding zero until its receipts land.
func (api *subscribeAPI) header(ctx context.Context, height atypes.GlobalBlockNumber, window atypes.ExecutedBlocks) (map[string]any, error) {
	h := coretypes.Int64(utils.Clamp[int64](height))
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &h})
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil {
		return nil, fmt.Errorf("block %d not found", height)
	}
	committed, ok := window.Get(height)
	if !ok {
		gasUsed, err := blockGasUsed(ctx, api.store, block)
		if err != nil {
			return nil, err
		}
		committed.GasUsed = gasUsed
	}
	return encodeHeader(api.backend, block, committed.GasUsed)
}
