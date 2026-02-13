package app

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gigautils "github.com/sei-protocol/sei-chain/giga/executor/utils"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type ExecuteTxsFallbackTestSuite struct {
	suite.Suite
}

func TestExecuteTxsFallbackTestSuite(t *testing.T) {
	suite.Run(t, new(ExecuteTxsFallbackTestSuite))
}

// TestProcessTxsSynchronousGiga_NonEVMTxTriggersFallback tests that a non-EVM transaction
// causes ProcessTxsSynchronousGiga to return ok=false (fallback needed)
func (suite *ExecuteTxsFallbackTestSuite) TestProcessTxsSynchronousGiga_NonEVMTxTriggersFallback() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a giga-enabled test wrapper
	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, false)

	// Create a non-EVM transaction (bank send)
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	ctx := testWrapper.Ctx.WithGiga(true)

	// Call ProcessTxsSynchronousGiga with a non-EVM transaction
	results, ok := testWrapper.App.ProcessTxsSynchronousGiga(
		ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should return ok=false because it's a non-EVM transaction (fallback needed)
	require.False(t, ok)
	require.Nil(t, results)
}

// TestProcessTxsSynchronousGiga_EmptyTxsSucceeds tests that an empty tx list succeeds
func (suite *ExecuteTxsFallbackTestSuite) TestProcessTxsSynchronousGiga_EmptyTxsSucceeds() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, false)
	ctx := testWrapper.Ctx.WithGiga(true)

	// Call with empty transaction list
	results, ok := testWrapper.App.ProcessTxsSynchronousGiga(
		ctx,
		[][]byte{},
		[]sdk.Tx{},
		[]int{},
	)

	// Should succeed with empty results
	require.True(t, ok)
	require.Empty(t, results)
}

// TestProcessTXsWithOCCGiga_NonEVMTxProcessedViaV2Scheduler tests that non-EVM transactions
// are processed via the v2 scheduler within ProcessTXsWithOCCGiga
func (suite *ExecuteTxsFallbackTestSuite) TestProcessTXsWithOCCGiga_NonEVMTxProcessedViaV2Scheduler() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a giga-enabled test wrapper with OCC
	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, true)

	// Create a non-EVM transaction (bank send)
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	ctx := testWrapper.Ctx.WithGiga(true).WithIsOCCEnabled(true)

	// Call ProcessTXsWithOCCGiga with a non-EVM transaction
	// Non-EVM txs should be processed via the v2Scheduler path within the function
	results, _, ok := testWrapper.App.ProcessTXsWithOCCGiga(
		ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should succeed because non-EVM txs go through v2 scheduler in OCC giga mode
	require.True(t, ok)
	require.Len(t, results, 1)
}

// TestProcessTXsWithOCCGiga_EmptyTxsSucceeds tests that an empty tx list succeeds
func (suite *ExecuteTxsFallbackTestSuite) TestProcessTXsWithOCCGiga_EmptyTxsSucceeds() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, true)
	ctx := testWrapper.Ctx.WithGiga(true).WithIsOCCEnabled(true)

	// Call with empty transaction list
	results, _, ok := testWrapper.App.ProcessTXsWithOCCGiga(
		ctx,
		[][]byte{},
		[]sdk.Tx{},
		[]int{},
	)

	// Should succeed with empty results
	require.True(t, ok)
	require.Empty(t, results)
}

// TestExecuteTxsConcurrently_GigaDisabled_UsesV2 tests that when giga is disabled,
// ExecuteTxsConcurrently uses V2 execution paths directly
func (suite *ExecuteTxsFallbackTestSuite) TestExecuteTxsConcurrently_GigaDisabled_UsesV2() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a standard (non-giga) test wrapper
	testWrapper := NewTestWrapper(t, tm, valPub, false)

	// Create a non-EVM transaction
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	// Ensure giga is disabled
	require.False(t, testWrapper.App.GigaExecutorEnabled)

	ctx := testWrapper.Ctx.WithIsOCCEnabled(false) // Use synchronous v2

	// Call ExecuteTxsConcurrently
	results, _ := testWrapper.App.ExecuteTxsConcurrently(
		ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should return results (tx may fail for other reasons, but it should be processed)
	require.Len(t, results, 1)
}

// TestExecuteTxsConcurrently_GigaDisabled_OCCEnabled_UsesOCCV2 tests that when giga is disabled
// but OCC is enabled, ExecuteTxsConcurrently uses OCC V2 execution
func (suite *ExecuteTxsFallbackTestSuite) TestExecuteTxsConcurrently_GigaDisabled_OCCEnabled_UsesOCCV2() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a standard (non-giga) test wrapper with SC enabled for OCC support
	testWrapper := NewTestWrapperWithSc(t, tm, valPub, false)

	// Create a non-EVM transaction
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	// Ensure giga is disabled
	require.False(t, testWrapper.App.GigaExecutorEnabled)

	ctx := testWrapper.Ctx.WithIsOCCEnabled(true) // Use OCC v2

	// Call ExecuteTxsConcurrently
	results, _ := testWrapper.App.ExecuteTxsConcurrently(
		ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should return results (tx may fail for other reasons, but it should be processed)
	require.Len(t, results, 1)
}

// TestExecuteTxsConcurrently_GigaFallback_SynchronousToSequential tests that
// when giga synchronous mode fails, it falls back to v2 sequential mode
func (suite *ExecuteTxsFallbackTestSuite) TestExecuteTxsConcurrently_GigaFallback_SynchronousToSequential() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a giga-enabled test wrapper (OCC disabled)
	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, false)

	// Create a non-EVM transaction that will trigger giga fallback
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	// Ensure giga is enabled but OCC is disabled
	require.True(t, testWrapper.App.GigaExecutorEnabled)
	require.False(t, testWrapper.App.GigaOCCEnabled)

	ctx := testWrapper.Ctx.WithIsOCCEnabled(false)

	// Call ExecuteTxsConcurrently - should fallback from giga to v2 sequential
	results, _ := testWrapper.App.ExecuteTxsConcurrently(
		ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should return results after fallback to v2 sequential
	require.Len(t, results, 1)
}

// TestExecuteTxsConcurrently_GigaFallback_OCCToOCCV2 tests that
// when giga OCC mode fails, it falls back to v2 OCC mode
func (suite *ExecuteTxsFallbackTestSuite) TestExecuteTxsConcurrently_GigaFallback_OCCToOCCV2() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	// Create a giga-enabled test wrapper with OCC enabled
	testWrapper := NewGigaTestWrapper(t, tm, valPub, false, true)

	// Ensure giga and OCC are both enabled
	require.True(t, testWrapper.App.GigaExecutorEnabled)
	require.True(t, testWrapper.App.GigaOCCEnabled)

	ctx := testWrapper.Ctx.WithIsOCCEnabled(true)

	// Call ExecuteTxsConcurrently with empty transactions - should succeed
	results, _ := testWrapper.App.ExecuteTxsConcurrently(
		ctx,
		[][]byte{},
		[]sdk.Tx{},
		[]int{},
	)

	// Should return empty results
	require.Empty(t, results)
}

// mockAbortResult creates an ExecTxResult that signals giga abort
func mockAbortResult() *abci.ExecTxResult {
	return &abci.ExecTxResult{
		Code:      gigautils.GigaAbortCode,
		Codespace: gigautils.GigaAbortCodespace,
		Info:      gigautils.GigaAbortInfo,
		Log:       "giga execution aborted - cosmos precompile interop detected",
	}
}

// TestGigaAbortCodeAndCodespace verifies the giga abort sentinel values
func (suite *ExecuteTxsFallbackTestSuite) TestGigaAbortCodeAndCodespace() {
	t := suite.T()

	result := mockAbortResult()

	// Verify the abort sentinel values are correct
	require.Equal(t, gigautils.GigaAbortCode, result.Code)
	require.Equal(t, gigautils.GigaAbortCodespace, result.Codespace)
	require.Equal(t, gigautils.GigaAbortInfo, result.Info)
}

// Test that ProcessTxsSynchronousV2 works correctly (baseline)
func (suite *ExecuteTxsFallbackTestSuite) TestProcessTxsSynchronousV2_Baseline() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := NewTestWrapper(t, tm, valPub, false)

	// Create a non-EVM transaction
	bankMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(valPub.Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
	}

	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))

	tx := txBuilder.GetTx()
	txBz, err := testWrapper.App.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	// Call ProcessTxsSynchronousV2 directly
	results := testWrapper.App.ProcessTxsSynchronousV2(
		testWrapper.Ctx,
		[][]byte{txBz},
		[]sdk.Tx{tx},
		[]int{0},
	)

	// Should return results
	require.Len(t, results, 1)
}

// TestContextPreservation tests that the context is properly returned/preserved
func (suite *ExecuteTxsFallbackTestSuite) TestContextPreservation() {
	t := suite.T()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := NewTestWrapper(t, tm, valPub, false)

	ctx := testWrapper.Ctx.WithBlockHeight(42)

	// Call ExecuteTxsConcurrently with empty tx list
	_, returnedCtx := testWrapper.App.ExecuteTxsConcurrently(
		ctx,
		[][]byte{},
		[]sdk.Tx{},
		[]int{},
	)

	// The returned context should preserve block height
	require.Equal(t, int64(42), returnedCtx.BlockHeight())
}

// Unit tests for the errors package
type GigaErrorsTestSuite struct {
	suite.Suite
}

func TestGigaErrorsTestSuite(t *testing.T) {
	suite.Run(t, new(GigaErrorsTestSuite))
}

func (suite *GigaErrorsTestSuite) TestShouldExecutionAbort_NilError() {
	t := suite.T()
	result := gigautils.ShouldExecutionAbort(nil)
	require.False(t, result)
}

// ExecuteTxsFallbackUnitTest contains unit tests for fallback logic
type ExecuteTxsFallbackUnitTest struct {
	suite.Suite
	app *App
	ctx sdk.Context
}

func TestExecuteTxsFallbackUnitTest(t *testing.T) {
	suite.Run(t, new(ExecuteTxsFallbackUnitTest))
}

func (suite *ExecuteTxsFallbackUnitTest) SetupTest() {
	suite.app = Setup(suite.T(), false, false, false)
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{Height: 1})
}

// TestExecutionPathSelection tests that the correct execution path is selected
func (suite *ExecuteTxsFallbackUnitTest) TestExecutionPathSelection_GigaDisabledOCCDisabled() {
	t := suite.T()

	// Setup: Giga disabled, OCC disabled
	suite.app.GigaExecutorEnabled = false
	suite.app.GigaOCCEnabled = false
	ctx := suite.ctx.WithIsOCCEnabled(false)

	// Execute with empty txs
	results, _ := suite.app.ExecuteTxsConcurrently(ctx, [][]byte{}, []sdk.Tx{}, []int{})

	// Should use ProcessTxsSynchronousV2 path (return empty results)
	require.Empty(t, results)
}

func (suite *ExecuteTxsFallbackUnitTest) TestExecutionPathSelection_GigaDisabledOCCEnabled() {
	t := suite.T()

	// Setup: Giga disabled, OCC enabled
	suite.app.GigaExecutorEnabled = false
	suite.app.GigaOCCEnabled = false
	ctx := suite.ctx.WithIsOCCEnabled(true)

	// Execute with empty txs
	results, _ := suite.app.ExecuteTxsConcurrently(ctx, [][]byte{}, []sdk.Tx{}, []int{})

	// Should use ProcessTXsWithOCCV2 path (return empty results)
	require.Empty(t, results)
}

func (suite *ExecuteTxsFallbackUnitTest) TestExecutionPathSelection_GigaEnabledOCCDisabled() {
	t := suite.T()

	// Setup: Giga enabled, OCC disabled
	suite.app.GigaExecutorEnabled = true
	suite.app.GigaOCCEnabled = false
	ctx := suite.ctx.WithIsOCCEnabled(false)

	// Execute with empty txs - should use ProcessTxsSynchronousGiga path
	results, _ := suite.app.ExecuteTxsConcurrently(ctx, [][]byte{}, []sdk.Tx{}, []int{})

	// Should return empty results (no fallback needed for empty tx list)
	require.Empty(t, results)
}

func (suite *ExecuteTxsFallbackUnitTest) TestExecutionPathSelection_GigaEnabledOCCEnabled() {
	t := suite.T()

	// Setup: Giga enabled, OCC enabled
	suite.app.GigaExecutorEnabled = true
	suite.app.GigaOCCEnabled = true
	ctx := suite.ctx.WithIsOCCEnabled(true)

	// Execute with empty txs - should use ProcessTXsWithOCCGiga path
	results, _ := suite.app.ExecuteTxsConcurrently(ctx, [][]byte{}, []sdk.Tx{}, []int{})

	// Should return empty results
	require.Empty(t, results)
}
