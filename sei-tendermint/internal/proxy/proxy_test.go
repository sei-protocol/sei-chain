package proxy

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

type testApp struct {
	types.BaseApplication
	checkTx func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2
}

func (app testApp) CheckTx(ctx context.Context, req *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
	return app.checkTx(ctx, req)
}

func TestCheckTxSafeReturnsErrorOnPanic(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			panic("boom")
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func validEVMResponse() *types.ResponseCheckTxV2 {
	return &types.ResponseCheckTxV2{
		ResponseCheckTx:    &types.ResponseCheckTx{Code: types.CodeTypeOK},
		EVMHash:            common.HexToHash("0x123"),
		IsEVM:              true,
		SeiSenderAddress:   sdk.AccAddress("sender"),
		EVMRequiredBalance: *uint256.NewInt(1),
	}
}

func TestCheckTxSafeReturnsErrorOnMissingEVMHash(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			res := validEVMResponse()
			res.EVMHash = common.Hash{}
			return res
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func TestCheckTxSafeReturnsErrorOnMissingSeiSenderAddress(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			res := validEVMResponse()
			res.SeiSenderAddress = nil
			return res
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func TestCheckTxSafeAllowsValidEVMResponse(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			return validEVMResponse()
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.NoError(t, err)
}

func TestEvmCallErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmCall(t.Context(), &core.Message{})

	require.Error(t, err)
}

type testEvmCallerApp struct {
	testApp
	call func(context.Context, *core.Message) (*core.ExecutionResult, error)
}

func (app testEvmCallerApp) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	return app.call(ctx, msg)
}

func TestEvmCallDelegatesToASupportingApplication(t *testing.T) {
	want := &core.ExecutionResult{ReturnData: []byte{0x2a}}
	var gotMsg *core.Message
	proxyApp := New(testEvmCallerApp{
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			gotMsg = msg
			return want, nil
		},
	})
	msg := &core.Message{GasLimit: 21_000}

	got, err := proxyApp.EvmCall(t.Context(), msg)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.Same(t, msg, gotMsg)
}

func TestEvmChainConfigErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmChainConfig()

	require.Error(t, err)
}

type testEvmChainConfigApp struct {
	testApp
	chainConfig *params.ChainConfig
}

func (app testEvmChainConfigApp) EvmChainConfig() *params.ChainConfig {
	return app.chainConfig
}

func TestEvmChainConfigDelegatesToASupportingApplication(t *testing.T) {
	want := &params.ChainConfig{ChainID: big.NewInt(713715)}
	proxyApp := New(testEvmChainConfigApp{chainConfig: want})

	got, err := proxyApp.EvmChainConfig()

	require.NoError(t, err)
	require.Same(t, want, got)
}
