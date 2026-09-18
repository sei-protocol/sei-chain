package rpc

import (
	"context"
	"errors"

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
	last := executed.Load().Number
	// The request ctx is canceled as soon as eth_subscribe returns; the stream
	// lives until rpcSub.Err() closes instead.
	go api.streamHeads(context.WithoutCancel(ctx), notifier, rpcSub, executed, last)
	return rpcSub, nil
}

func (api *subscribeAPI) streamHeads(
	ctx context.Context,
	notifier *ethrpc.Notifier,
	rpcSub *ethrpc.Subscription,
	executed utils.AtomicRecv[atypes.ExecutedBlock],
	last atypes.GlobalBlockNumber,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		// Err closes on eth_unsubscribe and when the connection drops; that is
		// the only way this stream ends.
		<-rpcSub.Err()
		cancel()
	}()
	for next := last + 1; ; next++ {
		committed, err := executed.Wait(ctx, func(b atypes.ExecutedBlock) bool { return b.Number >= next })
		if err != nil {
			return
		}
		header, err := api.header(ctx, next, committed)
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

// header renders the header of the block at height. gasUsed comes from the
// commit that published height; when the watch has moved past it, the receipt
// store is read instead, and yields zero if the receipts have not landed yet.
func (api *subscribeAPI) header(ctx context.Context, height atypes.GlobalBlockNumber, committed atypes.ExecutedBlock) (map[string]any, error) {
	h := coretypes.Int64(utils.Clamp[int64](height))
	block, err := api.backend.Block(ctx, &coretypes.RequestBlockInfo{Height: &h})
	if err != nil {
		return nil, err
	}
	gasUsed := committed.GasUsed
	if committed.Number != height {
		if gasUsed, err = blockGasUsed(ctx, api.store, block); err != nil {
			return nil, err
		}
	}
	return encodeHeader(api.backend, block, gasUsed)
}
