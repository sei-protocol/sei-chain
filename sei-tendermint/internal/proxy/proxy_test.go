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

func TestEvmCodeErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmCode(common.Address{1})

	require.Error(t, err)
}

type testEvmCodeReaderApp struct {
	testApp
	code func(common.Address) []byte
}

func (app testEvmCodeReaderApp) EvmCode(addr common.Address) []byte {
	return app.code(addr)
}

func TestEvmCodeDelegatesToASupportingApplication(t *testing.T) {
	want := []byte{0x60, 0x00, 0xf3}
	var gotAddr common.Address
	proxyApp := New(testEvmCodeReaderApp{
		code: func(addr common.Address) []byte {
			gotAddr = addr
			return want
		},
	})
	addr := common.Address{7}

	got, err := proxyApp.EvmCode(addr)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, addr, gotAddr)
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

func TestEvmBaseFeeErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmBaseFee()

	require.Error(t, err)
}

func TestEvmGasLimitErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmGasLimit()

	require.Error(t, err)
}

type testEvmBaseFeeApp struct {
	testApp
	baseFee *big.Int
}

func (app testEvmBaseFeeApp) EvmBaseFee() *big.Int {
	return app.baseFee
}

func TestEvmBaseFeeDelegatesToASupportingApplication(t *testing.T) {
	want := big.NewInt(7)
	proxyApp := New(testEvmBaseFeeApp{baseFee: want})

	got, err := proxyApp.EvmBaseFee()

	require.NoError(t, err)
	require.Same(t, want, got)
}

type testEvmGasLimitApp struct {
	testApp
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
	testApp
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

func TestEvmChainIDDelegatesToApplication(t *testing.T) {
	proxyApp := New(testApp{})

	// Test: EvmChainID is on Application, so every wrapped app exposes it.
	got := proxyApp.EvmChainID()

	// Verify: BaseApplication's zero chain ID is returned.
	require.Equal(t, uint64(0), got)
}
