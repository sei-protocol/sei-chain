package proxy

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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

// testEVMApp answers every EVMApplication method with a fixed value and
// records the arguments it was called with.
type testEVMApp struct {
	EVMApplication
	gotMsg       *core.Message
	gotCalls     []*core.Message
	gotGasCap    uint64
	gotHeader    *ethtypes.Header
	gotHeight    int64
	gotTxs       [][]byte
	gotOverrides evmonly.CallOverrides
}

var _ EVMApplication = (*testEVMApp)(nil)

var (
	testExecutionResult = &core.ExecutionResult{ReturnData: []byte{0x2a}}
	testAccessList      = ethtypes.AccessList{{Address: common.Address{0x01}, StorageKeys: []common.Hash{{0x02}}}}
	testChainConfig     = &params.ChainConfig{ChainID: big.NewInt(713715)}
	testBaseFee         = big.NewInt(7)
	testMinGasPrice     = big.NewInt(1_000_000_000)
	testOverrides       = evmonly.CallOverrides{State: export.StateOverride{common.Address{1}: {}}}
)

func (app *testEVMApp) EvmCall(_ context.Context, msg *core.Message, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	app.gotOverrides = overrides
	app.gotMsg = msg
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGas(_ context.Context, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotGasCap = msg, gasCap
	return 21_000, []byte{0x01}, nil
}

func (app *testEVMApp) EvmCallAt(_ context.Context, msg *core.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotHeader = msg, header
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGasAt(_ context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotGasCap, app.gotHeader = msg, gasCap, header
	return 22_000, []byte{0x02}, nil
}

func (app *testEVMApp) EvmEstimateGasAfterCalls(_ context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	app.gotCalls, app.gotMsg, app.gotGasCap = calls, msg, gasCap
	return 42_000, []byte{0x02}, nil
}

func (app *testEVMApp) EvmCreateAccessList(_ context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	app.gotMsg = msg
	return testAccessList, testExecutionResult, nil
}

func (app *testEVMApp) EvmChainID() uint64                  { return 713715 }
func (app *testEVMApp) EvmChainConfig() *params.ChainConfig { return testChainConfig }
func (app *testEVMApp) EvmBaseFee() *big.Int                { return testBaseFee }
func (app *testEVMApp) EvmGasLimit() uint64                 { return 35_000_000 }
func (app *testEVMApp) EvmMinGasPrice() *big.Int            { return testMinGasPrice }

func (app *testEVMApp) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	app.gotHeader = header
	return vm.BlockContext{BlockNumber: header.Number}, nil
}

func (app *testEVMApp) EvmStateAt(_ context.Context, height int64) (vm.SeiStateDB, func(), error) {
	app.gotHeight = height
	return nil, func() {}, nil
}

func (app *testEVMApp) EvmStateBeforeTx(_ context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error) {
	app.gotHeader, app.gotTxs = header, txs
	return nil, func() {}, nil
}

func TestEVMProxyDelegatesToTheApplication(t *testing.T) {
	// Setup: an EVMApplication wrapped by the proxy.
	app := &testEVMApp{}
	evm := NewEVM(app)
	msg := &core.Message{GasLimit: 21_000}

	t.Run("call", func(t *testing.T) {
		// Test: EvmCall.
		got, err := evm.EvmCall(t.Context(), msg, testOverrides)

		// Verify: the app's result is returned for the message passed in.
		require.NoError(t, err)
		require.Same(t, testExecutionResult, got)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
	})

	t.Run("estimate gas", func(t *testing.T) {
		// Test: EvmEstimateGas with a gas cap.
		got, revert, err := evm.EvmEstimateGas(t.Context(), msg, 10_000_000, testOverrides)

		// Verify: the app's estimate and revert data are returned for the message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(21_000), got)
		require.Equal(t, []byte{0x01}, revert)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("past block call", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(5)}

		// Test: EvmCallAt.
		got, err := evm.EvmCallAt(t.Context(), msg, header, testOverrides)

		// Verify: the app's result is returned for the message and block passed in.
		require.NoError(t, err)
		require.Same(t, testExecutionResult, got)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Same(t, header, app.gotHeader)
	})

	t.Run("past block gas estimate", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(6)}

		// Test: EvmEstimateGasAt with a gas cap.
		got, revert, err := evm.EvmEstimateGasAt(t.Context(), msg, 9_000_000, header, testOverrides)

		// Verify: the app's estimate and revert data are returned for the message, cap and block.
		require.NoError(t, err)
		require.Equal(t, uint64(22_000), got)
		require.Equal(t, []byte{0x02}, revert)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Equal(t, uint64(9_000_000), app.gotGasCap)
		require.Same(t, header, app.gotHeader)
	})

	t.Run("estimate gas after calls", func(t *testing.T) {
		// Test: EvmEstimateGasAfterCalls with one prior call and a gas cap.
		calls := []*core.Message{{GasLimit: 30_000}}
		got, revert, err := evm.EvmEstimateGasAfterCalls(t.Context(), calls, msg, 10_000_000)

		// Verify: the app's estimate and revert data are returned for the calls, message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(42_000), got)
		require.Equal(t, []byte{0x02}, revert)
		require.Equal(t, calls, app.gotCalls)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("create access list", func(t *testing.T) {
		// Test: EvmCreateAccessList.
		accessList, result, err := evm.EvmCreateAccessList(t.Context(), msg)

		// Verify: the app's access list and result are returned for the message passed in.
		require.NoError(t, err)
		require.Equal(t, testAccessList, accessList)
		require.Same(t, testExecutionResult, result)
		require.Same(t, msg, app.gotMsg)
	})

	t.Run("execution parameters", func(t *testing.T) {
		// Verify: chain ID, chain config, base fee, gas limit and min gas price come from the app.
		require.Equal(t, uint64(713715), evm.EvmChainID())
		require.Same(t, testChainConfig, evm.EvmChainConfig())
		require.Same(t, testBaseFee, evm.EvmBaseFee())
		require.Equal(t, uint64(35_000_000), evm.EvmGasLimit())
		require.Same(t, testMinGasPrice, evm.EvmMinGasPrice())
	})

	t.Run("trace reads", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(5)}
		txs := [][]byte{{1}, {2}}

		// Test: the block context, the state after a block, and the state before a transaction.
		blockCtx, err := evm.EvmBlockContext(header)
		require.NoError(t, err)
		_, release, err := evm.EvmStateAt(t.Context(), 4)
		require.NoError(t, err)
		require.NotNil(t, release)
		require.Equal(t, int64(4), app.gotHeight)
		_, release, err = evm.EvmStateBeforeTx(t.Context(), header, txs)

		// Verify: each is the app's answer for the arguments passed in.
		require.NoError(t, err)
		require.NotNil(t, release)
		require.Equal(t, header.Number, blockCtx.BlockNumber)
		require.Same(t, header, app.gotHeader)
		require.Equal(t, txs, app.gotTxs)
	})
}
