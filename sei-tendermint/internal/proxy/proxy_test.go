package proxy

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

func TestEvmGasLimitErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})
	_, err := proxyApp.EvmGasLimit()
	require.Error(t, err)
}

type testEvmGasLimitApp struct {
	types.BaseApplication
	gasLimit uint64
}

func (app testEvmGasLimitApp) EvmGasLimit() uint64 {
	return app.gasLimit
}

func TestEvmGasLimitDelegatesToASupportingApplication(t *testing.T) {
	proxyApp := New(testEvmGasLimitApp{gasLimit: 35_000_000})
	got, err := proxyApp.EvmGasLimit()
	require.NoError(t, err)
	require.Equal(t, uint64(35_000_000), got)
}

func TestEvmMinGasPriceErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})
	_, err := proxyApp.EvmMinGasPrice()
	require.Error(t, err)
}

type testEvmMinGasPriceApp struct {
	types.BaseApplication
	minGasPrice *big.Int
}

func (app testEvmMinGasPriceApp) EvmMinGasPrice() *big.Int {
	return app.minGasPrice
}

func TestEvmMinGasPriceDelegatesToASupportingApplication(t *testing.T) {
	want := big.NewInt(1_000_000_000)
	proxyApp := New(testEvmMinGasPriceApp{minGasPrice: want})
	got, err := proxyApp.EvmMinGasPrice()
	require.NoError(t, err)
	require.Equal(t, want, got)
}
