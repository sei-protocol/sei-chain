package proxy

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

type panicCheckTxApp struct {
	types.BaseApplication
}

func (panicCheckTxApp) CheckTx(_ context.Context, _ *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
	panic("boom")
}

func TestCheckTxSafeReturnsErrorOnPanic(t *testing.T) {
	proxyApp := New(panicCheckTxApp{}, NopMetrics())
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}
