package core

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testEVMApp answers every EVMApplication method with a fixed value and
// records the arguments it was called with.
type testEVMApp struct {
	abci.BaseApplication
	gotMsg       *ethcore.Message
	gotCalls     []*ethcore.Message
	gotGasCap    uint64
	gotHeader    *ethtypes.Header
	gotHeight    int64
	gotTxs       [][]byte
	gotOverrides evmonly.CallOverrides
}

var _ proxy.EVMApplication = (*testEVMApp)(nil)

var (
	testExecutionResult = &ethcore.ExecutionResult{ReturnData: []byte{0x2a}}
	testAccessList      = ethtypes.AccessList{{Address: common.Address{0x01}, StorageKeys: []common.Hash{{0x02}}}}
	testChainConfig     = &params.ChainConfig{ChainID: big.NewInt(713715)}
	testBaseFee         = big.NewInt(7)
	testMinGasPrice     = big.NewInt(1_000_000_000)
	testOverrides       = evmonly.CallOverrides{State: export.StateOverride{common.Address{1}: {}}}
)

func (app *testEVMApp) EvmCall(_ context.Context, msg *ethcore.Message, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	app.gotOverrides = overrides
	app.gotMsg = msg
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGas(_ context.Context, msg *ethcore.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotGasCap = msg, gasCap
	return 21_000, []byte{0x01}, nil
}

func (app *testEVMApp) EvmCallAt(_ context.Context, msg *ethcore.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotHeader = msg, header
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGasAt(_ context.Context, msg *ethcore.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	app.gotOverrides = overrides
	app.gotMsg, app.gotGasCap, app.gotHeader = msg, gasCap, header
	return 22_000, []byte{0x02}, nil
}

func (app *testEVMApp) EvmEstimateGasAfterCalls(_ context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64) (uint64, []byte, error) {
	app.gotCalls, app.gotMsg, app.gotGasCap = calls, msg, gasCap
	return 42_000, []byte{0x02}, nil
}

func (app *testEVMApp) EvmCreateAccessList(_ context.Context, msg *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
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

func (app *testEVMApp) EvmStateAt(_ context.Context, height int64) (vm.StateDB, func(), error) {
	app.gotHeight = height
	return nil, func() {}, nil
}

func (app *testEVMApp) EvmStateBeforeTx(_ context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	app.gotHeader, app.gotTxs = header, txs
	return nil, func() {}, nil
}

func TestEVMEnvironmentServesTheApplication(t *testing.T) {
	// Setup: an EVMEnvironment around a proxied EVMApplication, as node startup builds it.
	app := &testEVMApp{}
	proxyApp := proxy.New(app)
	evm, ok := proxyApp.EVM().Get()
	require.True(t, ok)
	env := &EVMEnvironment{Environment: &Environment{App: proxyApp}, EVM: evm}
	msg := &ethcore.Message{GasLimit: 21_000}

	t.Run("call", func(t *testing.T) {
		// Test: EvmCall.
		got, err := env.EvmCall(t.Context(), msg, testOverrides)

		// Verify: the app's result is returned for the message passed in.
		require.NoError(t, err)
		require.Equal(t, testExecutionResult, got)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
	})

	t.Run("estimate gas", func(t *testing.T) {
		// Test: EvmEstimateGas with a gas cap.
		got, revert, err := env.EvmEstimateGas(t.Context(), msg, 10_000_000, testOverrides)

		// Verify: the app's estimate and revert data are returned for the message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(21_000), got)
		require.Equal(t, []byte{0x01}, revert)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("past block call", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(5)}

		// Test: EvmCallAt.
		got, err := env.EvmCallAt(t.Context(), msg, header, testOverrides)

		// Verify: the app's result is returned for the message and block passed in.
		require.NoError(t, err)
		require.Equal(t, testExecutionResult, got)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Equal(t, header, app.gotHeader)
	})

	t.Run("past block gas estimate", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(6)}

		// Test: EvmEstimateGasAt with a gas cap.
		got, revert, err := env.EvmEstimateGasAt(t.Context(), msg, 9_000_000, header, testOverrides)

		// Verify: the app's estimate and revert data are returned for the message, cap and block.
		require.NoError(t, err)
		require.Equal(t, uint64(22_000), got)
		require.Equal(t, []byte{0x02}, revert)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, testOverrides, app.gotOverrides)
		require.Equal(t, uint64(9_000_000), app.gotGasCap)
		require.Equal(t, header, app.gotHeader)
	})

	t.Run("estimate gas after calls", func(t *testing.T) {
		// Test: EvmEstimateGasAfterCalls with one prior call and a gas cap.
		calls := []*ethcore.Message{{GasLimit: 30_000}}
		got, revert, err := env.EvmEstimateGasAfterCalls(t.Context(), calls, msg, 10_000_000)

		// Verify: the app's estimate and revert data are returned for the calls, message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(42_000), got)
		require.Equal(t, []byte{0x02}, revert)
		require.Equal(t, calls, app.gotCalls)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("create access list", func(t *testing.T) {
		// Test: EvmCreateAccessList.
		accessList, result, err := env.EvmCreateAccessList(t.Context(), msg)

		// Verify: the app's access list and result are returned for the message passed in.
		require.NoError(t, err)
		require.Equal(t, testAccessList, accessList)
		require.Equal(t, testExecutionResult, result)
		require.Equal(t, msg, app.gotMsg)
	})

	t.Run("execution parameters", func(t *testing.T) {
		// Test: the chain ID, chain config, base fee, gas limit and min gas price accessors.
		chainID := env.EvmChainID()
		chainConfig, chainConfigErr := env.EvmChainConfig()
		baseFee, baseFeeErr := env.EvmBaseFee()
		gasLimit, gasLimitErr := env.EvmGasLimit()
		minGasPrice, minGasPriceErr := env.EvmMinGasPrice()

		// Verify: each returns the app's value without error.
		require.Equal(t, uint64(713715), chainID)
		require.NoError(t, chainConfigErr)
		require.Equal(t, testChainConfig, chainConfig)
		require.NoError(t, baseFeeErr)
		require.Equal(t, testBaseFee, baseFee)
		require.NoError(t, gasLimitErr)
		require.Equal(t, uint64(35_000_000), gasLimit)
		require.NoError(t, minGasPriceErr)
		require.Equal(t, testMinGasPrice, minGasPrice)
	})

	t.Run("trace reads", func(t *testing.T) {
		header := &ethtypes.Header{Number: big.NewInt(5)}
		txs := [][]byte{{1}, {2}}

		// Test: the block context, the state after a block, and the state before a transaction.
		blockCtx, blockCtxErr := env.EvmBlockContext(header)
		_, releaseAt, stateAtErr := env.EvmStateAt(t.Context(), 4)
		gotHeight := app.gotHeight
		_, releaseBefore, stateBeforeErr := env.EvmStateBeforeTx(t.Context(), header, txs)

		// Verify: each is the app's answer for the arguments passed in.
		require.NoError(t, blockCtxErr)
		require.Equal(t, header.Number, blockCtx.BlockNumber)
		require.NoError(t, stateAtErr)
		require.True(t, releaseAt != nil)
		require.Equal(t, int64(4), gotHeight)
		require.NoError(t, stateBeforeErr)
		require.True(t, releaseBefore != nil)
		require.Equal(t, header, app.gotHeader)
		require.Equal(t, txs, app.gotTxs)
	})
}
