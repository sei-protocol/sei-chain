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
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// EvmProxy returns the EVM RPC URL of the autobahn validator that owns the
// sender shard, or None if the sender maps to the local validator (handle
// locally) or autobahn isn't configured.
func (env *Environment) EvmProxy(sender common.Address) utils.Option[*url.URL] {
	if r, ok := env.gigaRouter().Get(); ok {
		return r.EvmProxy(sender)
	}
	return utils.None[*url.URL]()
}

func (env *Environment) EvmTxByHash(hash common.Hash) (types.Tx, bool) {
	if giga, ok := env.gigaRouter().Get(); ok {
		if v, ok := giga.Mempool().Get(); ok {
			return v.EvmTxByHash(hash)
		}
		// Fullnode: no local mempool. The tx (if it exists locally at all)
		// would be at the shard owner; we can't easily query it from here.
		return nil, false
	}
	if mp, ok := env.Mempool.Get(); ok {
		return mp.EvmTxByHash(hash)
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
	if giga, ok := env.gigaRouter().Get(); ok {
		v, ok := giga.Mempool().Get()
		if !ok {
			return nil, errors.New("autobahn fullnode has no local mempool; broadcast_tx_* must be sent to a validator")
		}
		go func() { _, _ = v.TryInsertTx(ctx, req.Tx) }()
		return &coretypes.ResultBroadcastTx{Hash: req.Tx.Hash().Bytes()}, nil
	}
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	go func() { _, _ = mp.CheckTx(ctx, req.Tx) }()

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
	if giga, ok := env.gigaRouter().Get(); ok {
		v, ok := giga.Mempool().Get()
		if !ok {
			return nil, errors.New("autobahn fullnode has no local mempool; broadcast_tx_* must be sent to a validator")
		}
		r, err := v.InsertTx(ctx, req.Tx)
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
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	r, err := mp.CheckTx(ctx, req.Tx)
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
	if timeout := env.Config.TimeoutBroadcastTxCommit; timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if giga, ok := env.gigaRouter().Get(); ok {
		v, ok := giga.Mempool().Get()
		if !ok {
			return nil, errors.New("autobahn fullnode has no local mempool; broadcast_tx_* must be sent to a validator")
		}
		r, err := v.InsertTx(ctx, req.Tx)
		if err != nil {
			return nil, err
		}
		if r.Code != abci.CodeTypeOK {
			return &coretypes.ResultBroadcastTxCommit{
				CheckTx: *r,
				Hash:    req.Tx.Hash().Bytes(),
			}, nil
		}
		return env.broadcastTxCommitFromCheckTx(ctx, req, r)
	}
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	r, err := mp.CheckTx(ctx, req.Tx)
	if err != nil {
		return nil, err
	}
	return env.broadcastTxCommitFromCheckTx(ctx, req, r)
}

func (env *Environment) broadcastTxCommitFromCheckTx(ctx context.Context, req *coretypes.RequestBroadcastTx, r *abci.ResponseCheckTx) (*coretypes.ResultBroadcastTxCommit, error) {
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
				"err", ctx.Err())
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
	if giga, ok := env.gigaRouter().Get(); ok {
		v, ok := giga.Mempool().Get()
		if !ok {
			// Fullnode: no mempool; return empty (no pending txs locally).
			return &coretypes.ResultUnconfirmedTxs{}, nil
		}
		// NOTE: this pagination seems to be useless, given that the mempool content is
		// constantly changing and we don't have any snapshot marker in the request.
		rawTxs := v.UnconfirmedTxs()
		perPage := env.validatePerPage(req.PerPage.IntPtr())
		page, err := validatePage(req.Page.IntPtr(), perPage, len(rawTxs))
		if err != nil {
			return nil, err
		}
		skipCount := validateSkipCount(page, perPage)

		sizeBytes := 0
		for _, tx := range rawTxs {
			sizeBytes += len(tx)
		}
		var txs types.Txs
		for _, tx := range rawTxs[skipCount:min(skipCount+perPage, len(rawTxs))] {
			txs = append(txs, tx)
		}
		return &coretypes.ResultUnconfirmedTxs{
			Count:      len(txs),
			Total:      len(rawTxs),
			TotalBytes: int64(sizeBytes),
			Txs:        txs,
		}, nil
	}
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	txs := mp.RecentSnapshot()
	perPage := env.validatePerPage(req.PerPage.IntPtr())
	page, err := validatePage(req.Page.IntPtr(), perPage, len(txs))
	if err != nil {
		return nil, err
	}
	first := min(len(txs), validateSkipCount(page, perPage))
	next := first + min(len(txs)-first, perPage)
	result := txs[first:next]
	totalBytes := 0
	for _, tx := range txs {
		totalBytes += len(tx)
	}

	return &coretypes.ResultUnconfirmedTxs{
		Count:      len(result),
		Total:      len(txs),
		TotalBytes: int64(totalBytes),
		Txs:        result,
	}, nil
}

// NumUnconfirmedTxs gets number of unconfirmed transactions.
// More: https://docs.tendermint.com/master/rpc/#/Info/num_unconfirmed_txs
func (env *Environment) NumUnconfirmedTxs(ctx context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	if giga, ok := env.gigaRouter().Get(); ok {
		v, ok := giga.Mempool().Get()
		if !ok {
			return &coretypes.ResultUnconfirmedTxs{}, nil
		}
		rawTxs := v.UnconfirmedTxs()
		sizeBytes := 0
		for _, tx := range rawTxs {
			sizeBytes += len(tx)
		}
		return &coretypes.ResultUnconfirmedTxs{
			Count:      len(rawTxs),
			Total:      len(rawTxs),
			TotalBytes: int64(sizeBytes),
		}, nil
	}
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	total := mp.Size()
	return &coretypes.ResultUnconfirmedTxs{
		Count:      total,
		Total:      total,
		TotalBytes: utils.Clamp[int64](mp.SizeBytes()),
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
