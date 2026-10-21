package core

import (
	"context"
	"math/big"
	"testing"

	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testEVMApp answers every EVMApplication method with a fixed value and
// records the arguments it was called with.
type testEVMApp struct {
	abci.BaseApplication
	gotMsg    *ethcore.Message
	gotGasCap uint64
}

var _ proxy.EVMApplication = (*testEVMApp)(nil)

var (
	testExecutionResult = &ethcore.ExecutionResult{ReturnData: []byte{0x2a}}
	testChainConfig     = &params.ChainConfig{ChainID: big.NewInt(713715)}
	testBaseFee         = big.NewInt(7)
	testMinGasPrice     = big.NewInt(1_000_000_000)
)

func (app *testEVMApp) EvmCall(_ context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	app.gotMsg = msg
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGas(_ context.Context, msg *ethcore.Message, gasCap uint64) (uint64, []byte, error) {
	app.gotMsg, app.gotGasCap = msg, gasCap
	return 21_000, []byte{0x01}, nil
}

func (app *testEVMApp) EvmChainID() uint64                  { return 713715 }
func (app *testEVMApp) EvmChainConfig() *params.ChainConfig { return testChainConfig }
func (app *testEVMApp) EvmBaseFee() *big.Int                { return testBaseFee }
func (app *testEVMApp) EvmGasLimit() uint64                 { return 35_000_000 }
func (app *testEVMApp) EvmMinGasPrice() *big.Int            { return testMinGasPrice }

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
		got, err := env.EvmCall(t.Context(), msg)

		// Verify: the app's result is returned for the message passed in.
		require.NoError(t, err)
		require.Equal(t, testExecutionResult, got)
		require.Equal(t, msg, app.gotMsg)
	})

	t.Run("estimate gas", func(t *testing.T) {
		// Test: EvmEstimateGas with a gas cap.
		got, revert, err := env.EvmEstimateGas(t.Context(), msg, 10_000_000)

		// Verify: the app's estimate and revert data are returned for the message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(21_000), got)
		require.Equal(t, []byte{0x01}, revert)
		require.Equal(t, msg, app.gotMsg)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
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
}
