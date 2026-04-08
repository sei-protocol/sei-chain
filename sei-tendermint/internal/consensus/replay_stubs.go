package consensus

import (
	"context"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

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
