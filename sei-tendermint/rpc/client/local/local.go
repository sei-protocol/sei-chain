package local

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "rpc", "client", "local")

/*
Local is a Client implementation that directly executes the rpc
functions on a given node, without going through HTTP or GRPC.

This implementation is useful for:

* Running tests against a node in-process without the overhead
of going through an http server
* Communication between an ABCI app and Tendermint core when they
are compiled in process.

For real clients, you probably want to use client.HTTP.  For more
powerful control during testing, you probably want the "client/mock" package.

You can subscribe for any event published by Tendermint using Subscribe method.
Note delivery is best-effort. If you don't read events fast enough, Tendermint
might cancel the subscription. The client will attempt to resubscribe (you
don't need to do anything). It will keep trying indefinitely with exponential
backoff (10ms -> 20ms -> 40ms) until successful.
*/
type Local struct {
	*eventbus.EventBus
	*rpccore.Environment
}

// NodeService describes the portion of the node interface that the
// local RPC client constructor needs to build a local client.
type NodeService interface {
	service.Service
	RPCEnvironment() *rpccore.Environment
	EventBus() *eventbus.EventBus
}

// New configures a client that calls the Node directly.
func New(node NodeService) (*Local, error) {
	env := node.RPCEnvironment()
	if env == nil {
		return nil, errors.New("rpc is nil")
	}
	return &Local{
		EventBus:    node.EventBus(),
		Environment: env,
	}, nil
}

var _ rpcclient.Client = (*Local)(nil)

func (c *Local) ABCIQuery(ctx context.Context, path string, data bytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	return c.ABCIQueryWithOptions(ctx, path, data, rpcclient.DefaultABCIQueryOptions)
}

func (c *Local) ABCIQueryWithOptions(ctx context.Context, path string, data bytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	return c.Environment.ABCIQuery(ctx, &coretypes.RequestABCIQuery{
		Path: path, Data: data, Height: coretypes.Int64(opts.Height), Prove: opts.Prove,
	})
}

func (c *Local) BroadcastTxCommit(ctx context.Context, tx types.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	return c.Environment.BroadcastTxCommit(ctx, &coretypes.RequestBroadcastTx{Tx: tx})
}

func (c *Local) BroadcastTx(ctx context.Context, tx types.Tx) (*coretypes.ResultBroadcastTx, error) {
	return c.Environment.BroadcastTx(ctx, &coretypes.RequestBroadcastTx{Tx: tx})
}

func (c *Local) BroadcastTxAsync(ctx context.Context, tx types.Tx) (*coretypes.ResultBroadcastTx, error) {
	return c.Environment.BroadcastTxAsync(ctx, &coretypes.RequestBroadcastTx{Tx: tx})
}

func (c *Local) BroadcastTxSync(ctx context.Context, tx types.Tx) (*coretypes.ResultBroadcastTx, error) {
	return c.Environment.BroadcastTxSync(ctx, &coretypes.RequestBroadcastTx{Tx: tx})
}

func (c *Local) UnconfirmedTxs(ctx context.Context, page, perPage *int) (*coretypes.ResultUnconfirmedTxs, error) {
	return c.Environment.UnconfirmedTxs(ctx, &coretypes.RequestUnconfirmedTxs{
		Page:    coretypes.Int64Ptr(page),
		PerPage: coretypes.Int64Ptr(perPage),
	})
}

func (c *Local) CheckTx(ctx context.Context, tx types.Tx) (*coretypes.ResultCheckTx, error) {
	return c.Environment.CheckTx(ctx, &coretypes.RequestCheckTx{Tx: tx})
}

func (c *Local) EvmNextPendingNonce(addr common.Address) uint64 {
	if giga, ok := c.Environment.Router.Giga().Get(); ok {
		if v, ok := giga.Mempool().Get(); ok {
			return v.EvmNextPendingNonce(addr)
		}
		// Fullnode: no local mempool; the pending nonce lives on the
		// shard owner. Returning 0 here defers to the on-chain confirmed
		// nonce; callers that need the pending value should query the
		// shard owner's EVM RPC directly via EvmProxy.
		return 0
	}
	if mp, ok := c.Mempool.Get(); ok {
		return mp.EvmNextPendingNonce(addr)
	}
	return 0
}

func (c *Local) ConsensusState(ctx context.Context) (*coretypes.ResultConsensusState, error) {
	return c.GetConsensusState(ctx)
}

func (c *Local) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	return c.Environment.ConsensusParams(ctx, &coretypes.RequestConsensusParams{Height: (*coretypes.Int64)(height)})
}

func (c *Local) BlockchainInfo(ctx context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	return c.Environment.BlockchainInfo(ctx, &coretypes.RequestBlockchainInfo{
		MinHeight: coretypes.Int64(minHeight),
		MaxHeight: coretypes.Int64(maxHeight),
	})
}

func (c *Local) GenesisChunked(ctx context.Context, id uint) (*coretypes.ResultGenesisChunk, error) {
	return c.Environment.GenesisChunked(ctx, &coretypes.RequestGenesisChunked{Chunk: coretypes.Int64(id)}) //nolint:gosec // id is a small genesis chunk index
}

func (c *Local) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	return c.Environment.Block(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(height)})
}

func (c *Local) BlockByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return c.Environment.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: hash})
}

func (c *Local) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	return c.Environment.BlockResults(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(height)})
}

func (c *Local) Header(ctx context.Context, height *int64) (*coretypes.ResultHeader, error) {
	return c.Environment.Header(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(height)})
}

func (c *Local) HeaderByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultHeader, error) {
	return c.Environment.HeaderByHash(ctx, &coretypes.RequestBlockByHash{Hash: hash})
}

func (c *Local) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	return c.Environment.Commit(ctx, &coretypes.RequestBlockInfo{Height: (*coretypes.Int64)(height)})
}

func (c *Local) Validators(ctx context.Context, height *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	return c.Environment.Validators(ctx, &coretypes.RequestValidators{
		Height:  (*coretypes.Int64)(height),
		Page:    coretypes.Int64Ptr(page),
		PerPage: coretypes.Int64Ptr(perPage),
	})
}

func (c *Local) Tx(ctx context.Context, hash bytes.HexBytes, prove bool) (*coretypes.ResultTx, error) {
	return c.Environment.Tx(ctx, &coretypes.RequestTx{Hash: hash, Prove: prove})
}

func (c *Local) TxSearch(ctx context.Context, queryString string, prove bool, page, perPage *int, orderBy string) (*coretypes.ResultTxSearch, error) {
	return c.Environment.TxSearch(ctx, &coretypes.RequestTxSearch{
		Query:   queryString,
		Prove:   prove,
		Page:    coretypes.Int64Ptr(page),
		PerPage: coretypes.Int64Ptr(perPage),
		OrderBy: orderBy,
	})
}

func (c *Local) BlockSearch(ctx context.Context, queryString string, page, perPage *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	return c.Environment.BlockSearch(ctx, &coretypes.RequestBlockSearch{
		Query:   queryString,
		Page:    coretypes.Int64Ptr(page),
		PerPage: coretypes.Int64Ptr(perPage),
		OrderBy: orderBy,
	})
}

func (c *Local) BroadcastEvidence(ctx context.Context, ev types.Evidence) (*coretypes.ResultBroadcastEvidence, error) {
	return c.Environment.BroadcastEvidence(ctx, &coretypes.RequestBroadcastEvidence{Evidence: ev})
}

func (c *Local) Subscribe(ctx context.Context, subscriber, queryString string, capacity ...int) (<-chan coretypes.ResultEvent, error) {
	q, err := query.New(queryString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	limit, quota := 1, 0
	if len(capacity) > 0 {
		limit = capacity[0]
		if len(capacity) > 1 {
			quota = capacity[1]
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() { c.Wait(); cancel() }()

	subArgs := pubsub.SubscribeArgs{
		ClientID: subscriber,
		Query:    q,
		Quota:    quota,
		Limit:    limit,
	}
	sub, err := c.SubscribeWithArgs(ctx, subArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe: %w", err)
	}

	outc := make(chan coretypes.ResultEvent, 1)
	go c.eventsRoutine(ctx, sub, subArgs, outc)

	return outc, nil
}

func (c *Local) eventsRoutine(ctx context.Context, sub eventbus.Subscription, subArgs pubsub.SubscribeArgs, outc chan<- coretypes.ResultEvent) {
	qstr := subArgs.Query.String()
	for {
		msg, err := sub.Next(ctx)
		if errors.Is(err, pubsub.ErrUnsubscribed) {
			return // client unsubscribed
		} else if err != nil {
			logger.Error("subscription was canceled, resubscribing", "query", subArgs.Query, "err", err)
			sub = c.resubscribe(ctx, subArgs)
			if sub == nil {
				return // client terminated
			}
			continue
		}
		select {
		case outc <- coretypes.ResultEvent{
			SubscriptionID: msg.SubscriptionID(),
			Query:          qstr,
			Data:           msg.LegacyData(),
			Events:         msg.Events(),
		}:
		case <-ctx.Done():
			return
		}
	}
}

// Try to resubscribe with exponential backoff.
func (c *Local) resubscribe(ctx context.Context, subArgs pubsub.SubscribeArgs) eventbus.Subscription {
	timer := time.NewTimer(0)
	defer timer.Stop()

	attempts := 0
	for {
		if !c.IsRunning() {
			return nil
		}

		sub, err := c.SubscribeWithArgs(ctx, subArgs)
		if err == nil {
			return sub
		}

		attempts++
		timer.Reset((10 << min(uint(attempts), 31)) * time.Millisecond) //nolint:gosec // attempts is a small non-negative counter
		select {
		case <-timer.C:
			continue
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *Local) Unsubscribe(ctx context.Context, subscriber, queryString string) error {
	args := pubsub.UnsubscribeArgs{Subscriber: subscriber}
	var err error
	args.Query, err = query.New(queryString)
	if err != nil {
		// if this isn't a valid query it might be an ID, so
		// we'll try that. It'll turn into an error when we
		// try to unsubscribe. Eventually, perhaps, we'll want
		// to change the interface to only allow
		// unsubscription by ID, but that's a larger change.
		args.ID = queryString
	}
	return c.EventBus.Unsubscribe(ctx, args)
}

func (c *Local) UnsubscribeAll(ctx context.Context, subscriber string) error {
	return c.EventBus.UnsubscribeAll(ctx, subscriber)
}
