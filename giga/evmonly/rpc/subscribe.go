package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// logsRetryInterval is how long a logs subscription waits to poll again when an executed block's
// receipts are not indexed yet.
const logsRetryInterval = 100 * time.Millisecond

type subscribeAPI struct {
	backend Backend
	store   receipt.ReceiptStore
	logs    *filterAPI
	limits  *subscriptionLimiter
}

// logsErrorNotification is what a logs subscription pushes for a poll that failed. BlockNumber is
// the block whose logs were skipped when the subscription continues past it.
type logsErrorNotification struct {
	Error       string          `json:"error"`
	BlockNumber *hexutil.Uint64 `json:"blockNumber,omitempty"`
}

// Logs serves eth_subscribe("logs"), pushing each log crit matches as blocks execute, polled
// through the same cursor as eth_getFilterChanges. A failed poll pushes a logsErrorNotification;
// the stream continues only past a block whose logs alone exceed the log caps.
func (api *subscribeAPI) Logs(ctx context.Context, crit *filters.FilterCriteria) (*ethrpc.Subscription, error) {
	notifier, ok := ethrpc.NotifierFromContext(ctx)
	if !ok {
		return nil, ethrpc.ErrNotificationsUnsupported
	}
	criteria := subscriptionLogCriteria(crit)
	cursor, err := api.logs.pollStart(criteria)
	if err != nil {
		return nil, err
	}
	executed, err := api.backend.ExecutedBlocks()
	if err != nil {
		return nil, err
	}
	connection := ethrpc.PeerInfoFromContext(ctx).RemoteAddr
	if err := api.limits.acquireLogs(connection); err != nil {
		return nil, err
	}
	rpcSub := notifier.CreateSubscription()
	go func() {
		// The stream ends on eth_unsubscribe, when the connection drops, or once a bounded query
		// is delivered, and its slot frees with it.
		defer api.limits.releaseLogs(connection)
		api.streamLogs(context.WithoutCancel(ctx), notifier, rpcSub, executed, criteria, cursor)
	}()
	return rpcSub, nil
}

// subscriptionLogCriteria returns the criteria a logs subscription polls with. A fromBlock of 0
// with a tag toBlock, which ethclient sends for an open-ended query, is treated as an omitted
// fromBlock.
func subscriptionLogCriteria(crit *filters.FilterCriteria) filters.FilterCriteria {
	if crit == nil {
		return filters.FilterCriteria{}
	}
	if crit.FromBlock != nil && crit.FromBlock.Sign() == 0 && crit.ToBlock != nil && crit.ToBlock.Sign() < 0 {
		return filters.FilterCriteria{Addresses: crit.Addresses, Topics: crit.Topics}
	}
	return *crit
}

func (api *subscribeAPI) streamLogs(
	ctx context.Context,
	notifier *ethrpc.Notifier,
	rpcSub *ethrpc.Subscription,
	executed utils.AtomicRecv[atypes.ExecutedBlocks],
	crit filters.FilterCriteria,
	cursor uint64,
) {
	// This goroutine is outside go-ethereum's per-call recovery, so a panic ends the subscription
	// rather than the node.
	defer func() {
		if r := recover(); r != nil {
			logger.Error("logs: stream panicked", "subscription", rpcSub.ID, "panic", r)
		}
	}()
	ctx, cancel := subscriptionContext(ctx, rpcSub.Err())
	defer cancel()
	for {
		if _, err := executed.Wait(ctx, func(w atypes.ExecutedBlocks) bool { return uint64(w.Latest().Number) >= cursor }); err != nil {
			return
		}
		logs, next, err := api.logs.pollLogs(ctx, crit, cursor)
		if err != nil {
			if ctx.Err() != nil || notifier.Notify(rpcSub.ID, newLogsErrorNotification(err)) != nil {
				return
			}
			var oversized *oversizedBlockError
			if !errors.As(err, &oversized) {
				return
			}
		}
		for _, lg := range logs {
			if err := notifier.Notify(rpcSub.ID, lg); err != nil {
				return
			}
		}
		if logsExhausted(crit, next) {
			return
		}
		if next == cursor {
			select {
			case <-ctx.Done():
				return
			case <-time.After(logsRetryInterval):
			}
		}
		cursor = next
	}
}

// subscriptionContext returns a context derived from ctx that is cancelled once subErr closes,
// which happens on eth_unsubscribe and when the connection drops, or once cancel is called.
func subscriptionContext(ctx context.Context, subErr <-chan error) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-subErr:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func newLogsErrorNotification(err error) logsErrorNotification {
	notification := logsErrorNotification{Error: err.Error()}
	var oversized *oversizedBlockError
	if errors.As(err, &oversized) {
		height := hexutil.Uint64(oversized.height)
		notification.BlockNumber = &height
	}
	return notification
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
	// A WebSocket peer's address is unique among open connections.
	connection := ethrpc.PeerInfoFromContext(ctx).RemoteAddr
	if err := api.limits.acquireNewHeads(connection); err != nil {
		return nil, err
	}
	rpcSub := notifier.CreateSubscription()
	go func() {
		// Err closes on eth_unsubscribe and when the connection drops.
		<-rpcSub.Err()
		api.limits.releaseNewHeads(connection)
	}()
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
	ctx, cancel := subscriptionContext(ctx, rpcSub.Err())
	defer cancel()
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
