package consensus

import (
	"context"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

//-----------------------------------------------------------------------------

type emptyMempool struct{}

func (m emptyMempool) HasTx(txKey types.TxKey) bool {
	return false
}

func (m emptyMempool) GetTxsForKeys(txKeys []types.TxKey) types.Txs {
	return types.Txs{}
}

func (m emptyMempool) SafeGetTxsForKeys(txKeys []types.TxKey) (types.Txs, []types.TxKey) {
	return types.Txs{}, []types.TxKey{}
}

var _ mempool.Mempool = emptyMempool{}

func (emptyMempool) TxStore() *mempool.TxStore { return nil }
func (emptyMempool) Lock()                     {}
func (emptyMempool) Unlock()                   {}
func (emptyMempool) Size() int                 { return 0 }
func (emptyMempool) CheckTx(context.Context, types.Tx, func(*abci.ResponseCheckTx), mempool.TxInfo) error {
	return nil
}
func (emptyMempool) RemoveTxByKey(txKey types.TxKey) error      { return nil }
func (emptyMempool) ReapMaxBytesMaxGas(_, _, _ int64) types.Txs { return types.Txs{} }
func (emptyMempool) ReapMaxTxs(n int) types.Txs                 { return types.Txs{} }
func (emptyMempool) Update(
	_ context.Context,
	_ int64,
	_ types.Txs,
	_ []*abci.ExecTxResult,
	_ mempool.PreCheckFunc,
	_ mempool.PostCheckFunc,
	_ bool,
) error {
	return nil
}
func (emptyMempool) Flush()                        {}
func (emptyMempool) TxsAvailable() <-chan struct{} { return make(chan struct{}) }
func (emptyMempool) EnableTxsAvailable()           {}
func (emptyMempool) SizeBytes() int64              { return 0 }

func (emptyMempool) TxsFront() *clist.CElement    { return nil }
func (emptyMempool) TxsWaitChan() <-chan struct{} { return nil }

func (emptyMempool) InitWAL() error { return nil }
func (emptyMempool) CloseWAL()      {}

//-----------------------------------------------------------------------------
// mockProxyApp uses Responses to FinalizeBlock to give the right results.
//
// Useful because we don't want to call Commit() twice for the same block on
// the real app.

func newMockProxyApp(
	appHash []byte,
	finalizeBlockResponses *abci.ResponseFinalizeBlock,
) abci.Application {
	return &mockProxyApp{
		appHash:                appHash,
		finalizeBlockResponses: finalizeBlockResponses,
	}
}

type mockProxyApp struct {
	abci.BaseApplication

	appHash                []byte
	txCount                int
	finalizeBlockResponses *abci.ResponseFinalizeBlock
}

func (mock *mockProxyApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	r := mock.finalizeBlockResponses
	mock.txCount++
	if r == nil {
		return &abci.ResponseFinalizeBlock{}, nil
	}
	return r, nil
}

func (mock *mockProxyApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}
