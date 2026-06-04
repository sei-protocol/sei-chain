package core

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// EvmProxy returns the EVM RPC URL of the autobahn validator that owns the
// sender shard. If the sender maps to the local validator, or if no EVM RPC
// endpoint is configured for the shard owner, it returns (nil, false).
func (env *Environment) EvmProxy(sender common.Address) (*url.URL, bool) {
	if r, ok := env.gigaRouter().Get(); ok {
		return r.EvmProxy(sender)
	}
	return nil, false
}

//-----------------------------------------------------------------------------
// NOTE: tx should be signed, but this is only checked at the app level (not by Tendermint!)

// BroadcastTxAsync returns right away, with no response. Does not wait for
// CheckTx nor DeliverTx results.
// More:
// https://docs.tendermint.com/master/rpc/#/Tx/broadcast_tx_async
// Deprecated and should be removed in 0.37
func (env *Environment) BroadcastTxAsync(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	go func() { _, _ = env.Mempool.CheckTx(ctx, req.Tx) }()

	return &coretypes.ResultBroadcastTx{Hash: req.Tx.Hash().Bytes()}, nil
}

// Deprecated and should be remove in 0.37
func (env *Environment) BroadcastTxSync(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return env.BroadcastTx(ctx, req)
}

// BroadcastTx returns with the response from CheckTx. Does not wait for
// DeliverTx result.
// More: https://docs.tendermint.com/master/rpc/#/Tx/broadcast_tx_sync
func (env *Environment) BroadcastTx(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	r, err := env.Mempool.CheckTx(ctx, req.Tx)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultBroadcastTx{
		Code:      r.Code,
		Data:      r.Data,
		Codespace: r.Codespace,
		Hash:      req.Tx.Hash().Bytes(),
		Log:       r.Log,
	}, nil
}

// BroadcastTxCommit returns with the responses from CheckTx and DeliverTx.
// More: https://docs.tendermint.com/master/rpc/#/Tx/broadcast_tx_commit
func (env *Environment) BroadcastTxCommit(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTxCommit, error) {
	r, err := env.Mempool.CheckTx(ctx, req.Tx)
	if err != nil {
		return nil, err
	}
	if r.Code != abci.CodeTypeOK {
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx: *r,
			Hash:    req.Tx.Hash().Bytes(),
		}, nil
	}

	if !indexer.KVSinkEnabled(env.EventSinks) {
		return &coretypes.ResultBroadcastTxCommit{
				CheckTx: *r,
				Hash:    req.Tx.Hash().Bytes(),
			},
			errors.New("cannot confirm transaction because kvEventSink is not enabled")
	}

	startAt := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()

	count := 0
	for {
		count++
		select {
		case <-ctx.Done():
			logger.Error("error on broadcastTxCommit",
				"duration", time.Since(startAt),
				"err", err)
			return &coretypes.ResultBroadcastTxCommit{
					CheckTx: *r,
					Hash:    req.Tx.Hash().Bytes(),
				}, fmt.Errorf("timeout waiting for commit of tx %s (%s)",
					req.Tx.Hash(), time.Since(startAt))
		case <-timer.C:
			txres, err := env.Tx(ctx, &coretypes.RequestTx{
				Hash:  req.Tx.Hash().Bytes(),
				Prove: false,
			})
			if err != nil {
				jitter := 100*time.Millisecond + time.Duration(rand.Int63n(int64(time.Second))) // nolint: gosec
				backoff := 100 * time.Duration(count) * time.Millisecond
				timer.Reset(jitter + backoff)
				continue
			}

			return &coretypes.ResultBroadcastTxCommit{
				CheckTx:  *r,
				TxResult: txres.TxResult,
				Hash:     req.Tx.Hash().Bytes(),
				Height:   txres.Height,
			}, nil
		}
	}
}

// UnconfirmedTxs gets unconfirmed transactions from the mempool in order of priority
// More: https://docs.tendermint.com/master/rpc/#/Info/unconfirmed_txs
func (env *Environment) UnconfirmedTxs(ctx context.Context, req *coretypes.RequestUnconfirmedTxs) (*coretypes.ResultUnconfirmedTxs, error) {
	totalCount := env.Mempool.Size()
	perPage := env.validatePerPage(req.PerPage.IntPtr())
	page, err := validatePage(req.Page.IntPtr(), perPage, totalCount)
	if err != nil {
		return nil, err
	}

	txs := env.Mempool.RecentSnapshot()
	first := min(len(txs), validateSkipCount(page, perPage))
	next := first + min(len(txs)-first, perPage)
	result := txs[first:next]

	return &coretypes.ResultUnconfirmedTxs{
		Count:      len(result),
		Total:      totalCount,
		TotalBytes: utils.Clamp[int64](env.Mempool.SizeBytes()),
		Txs:        result,
	}, nil
}

// NumUnconfirmedTxs gets number of unconfirmed transactions.
// More: https://docs.tendermint.com/master/rpc/#/Info/num_unconfirmed_txs
func (env *Environment) NumUnconfirmedTxs(ctx context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	return &coretypes.ResultUnconfirmedTxs{
		Count:      env.Mempool.Size(),
		Total:      env.Mempool.Size(),
		TotalBytes: utils.Clamp[int64](env.Mempool.SizeBytes()),
	}, nil
}

// CheckTx checks the transaction without executing it. The transaction won't
// be added to the mempool either.
// More: https://docs.tendermint.com/master/rpc/#/Tx/check_tx
func (env *Environment) CheckTx(ctx context.Context, req *coretypes.RequestCheckTx) (*coretypes.ResultCheckTx, error) {
	res, err := env.App.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: req.Tx})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultCheckTx{ResponseCheckTx: *res.ResponseCheckTx}, nil
}
