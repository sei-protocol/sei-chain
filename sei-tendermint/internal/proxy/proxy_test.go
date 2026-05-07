package proxy

import (
	"context"
	"math/big"
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

type invalidEVMCheckTxApp struct {
	types.BaseApplication
}

func (invalidEVMCheckTxApp) CheckTx(_ context.Context, _ *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
	return &types.ResponseCheckTxV2{
		ResponseCheckTx: &types.ResponseCheckTx{Code: types.CodeTypeOK},
		IsEVM:           true,
	}
}

type validEVMCheckTxApp struct {
	types.BaseApplication
}

func (validEVMCheckTxApp) CheckTx(_ context.Context, _ *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
	return &types.ResponseCheckTxV2{
		ResponseCheckTx:    &types.ResponseCheckTx{Code: types.CodeTypeOK},
		IsEVM:              true,
		EVMRequiredBalance: big.NewInt(1),
	}
}

func TestCheckTxSafeReturnsErrorOnMissingEVMRequiredBalance(t *testing.T) {
	proxyApp := New(invalidEVMCheckTxApp{}, NopMetrics())
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func TestCheckTxSafeAllowsValidEVMResponse(t *testing.T) {
	proxyApp := New(validEVMCheckTxApp{}, NopMetrics())
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.NoError(t, err)
}
