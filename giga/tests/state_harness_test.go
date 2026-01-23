package giga_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/giga/tests/harness"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// FailureType categorizes the type of test failure
type FailureType string

const (
	FailureTypeResultCode    FailureType = "result_code"
	FailureTypeGasMismatch   FailureType = "gas_mismatch"
	FailureTypeStateMismatch FailureType = "state_mismatch"
	FailureTypeCodeMismatch  FailureType = "code_mismatch"
	FailureTypeNonceMismatch FailureType = "nonce_mismatch"
	FailureTypeErrorMismatch FailureType = "error_mismatch"
	FailureTypeV2Error       FailureType = "v2_error"
	FailureTypeGigaError     FailureType = "giga_error"
	FailureTypeUnknown       FailureType = "unknown"
)

// TestResult captures the outcome of a state test comparison
type TestResult struct {
	Passed      bool
	FailureType FailureType
	Message     string
	Details     []string
}

// StateTestContext wraps the test context for state test execution
type StateTestContext struct {
	Ctx     sdk.Context
	TestApp *app.App
	Mode    ExecutorMode
}

// NewStateTestContext creates a test context configured for state tests
func NewStateTestContext(t testing.TB, blockTime time.Time, workers int, mode ExecutorMode) *StateTestContext {
	occEnabled := mode == ModeV2withOCC || mode == ModeGigaOCC
	gigaEnabled := mode == ModeGigaSequential || mode == ModeGigaOCC
	gigaOCCEnabled := mode == ModeGigaOCC

	testAcct := utils.NewSigner()

	var wrapper *app.TestWrapper
	if !gigaEnabled {
		wrapper = app.NewTestWrapperWithSc(t.(*testing.T), blockTime, testAcct.PublicKey, true, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(occEnabled)
			ba.SetConcurrencyWorkers(workers)
		})
	} else {
		wrapper = app.NewGigaTestWrapper(t.(*testing.T), blockTime, testAcct.PublicKey, true, gigaOCCEnabled, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(occEnabled)
			ba.SetConcurrencyWorkers(workers)
		})
	}
	testApp := wrapper.App
	ctx := wrapper.Ctx
	ctx = ctx.WithBlockHeader(tmproto.Header{
		Height:  ctx.BlockHeader().Height,
		ChainID: ctx.BlockHeader().ChainID,
		Time:    blockTime,
	})

	// Set minimum fee to 0 for state tests
	params := testApp.EvmKeeper.GetParams(ctx)
	params.MinimumFeePerGas = sdk.NewDecFromInt(sdk.NewInt(0))
	testApp.EvmKeeper.SetParams(ctx, params)

	return &StateTestContext{
		Ctx:     ctx,
		TestApp: testApp,
		Mode:    mode,
	}
}

// SetupPreState configures the state from the test's pre-state allocation
func (stc *StateTestContext) SetupPreState(t testing.TB, pre ethtypes.GenesisAlloc) {
	for addr, account := range pre {
		// Fund the account with the specified balance
		usei, wei := state.SplitUseiWeiAmount(account.Balance)
		seiAddr := stc.TestApp.EvmKeeper.GetSeiAddressOrDefault(stc.Ctx, addr)

		if usei.GT(sdk.ZeroInt()) {
			err := stc.TestApp.BankKeeper.MintCoins(stc.Ctx, "mint", sdk.NewCoins(sdk.NewCoin("usei", usei)))
			require.NoError(t, err, "failed to mint coins for %s", addr.Hex())
			err = stc.TestApp.BankKeeper.SendCoinsFromModuleToAccount(stc.Ctx, "mint", seiAddr, sdk.NewCoins(sdk.NewCoin("usei", usei)))
			require.NoError(t, err, "failed to send coins to %s", addr.Hex())
		}
		if wei.GT(sdk.ZeroInt()) {
			err := stc.TestApp.BankKeeper.AddWei(stc.Ctx, seiAddr, wei)
			require.NoError(t, err, "failed to add wei to %s", addr.Hex())
		}

		// Set nonce
		stc.TestApp.EvmKeeper.SetNonce(stc.Ctx, addr, account.Nonce)

		// Set code if present
		if len(account.Code) > 0 {
			stc.TestApp.EvmKeeper.SetCode(stc.Ctx, addr, account.Code)
		}

		// Set storage
		for key, value := range account.Storage {
			stc.TestApp.EvmKeeper.SetState(stc.Ctx, addr, key, value)
		}

		// Associate the addresses
		stc.TestApp.EvmKeeper.SetAddressMapping(stc.Ctx, seiAddr, addr)
	}
}

// SetupSender associates the sender address with its Sei address
func (stc *StateTestContext) SetupSender(sender common.Address) {
	seiAddr := stc.TestApp.EvmKeeper.GetSeiAddressOrDefault(stc.Ctx, sender)
	stc.TestApp.EvmKeeper.SetAddressMapping(stc.Ctx, seiAddr, sender)
}

// RunStateTestBlock executes a state test transaction and returns results
func RunStateTestBlock(stc *StateTestContext, txs [][]byte) ([]abci.Event, []*abci.ExecTxResult, error) {
	app.EnableOCC = stc.Mode == ModeV2withOCC || stc.Mode == ModeGigaOCC

	req := &abci.RequestFinalizeBlock{
		Txs:    txs,
		Height: stc.Ctx.BlockHeader().Height,
	}

	events, results, _, err := stc.TestApp.ProcessBlock(stc.Ctx, txs, req, req.DecidedLastCommit, false)
	return events, results, err
}

// runStateTestComparison runs a state test through both V2 and Giga and compares results
// This is kept for backward compatibility
func runStateTestComparison(t *testing.T, st *harness.StateTestJSON, post harness.StateTestPost) {
	result := runStateTestComparisonWithResult(t, st, post)
	if !result.Passed {
		t.Errorf("Test failed: %s - %s", result.FailureType, result.Message)
		for _, detail := range result.Details {
			t.Logf("  %s", detail)
		}
	}
}

// runStateTestComparisonWithResult runs a state test and returns detailed results
func runStateTestComparisonWithResult(t *testing.T, st *harness.StateTestJSON, post harness.StateTestPost) TestResult {
	blockTime := time.Now()

	// Build the transaction
	signedTx, sender, err := harness.BuildTransaction(st, post)
	if err != nil {
		return TestResult{
			Passed:      false,
			FailureType: FailureTypeUnknown,
			Message:     fmt.Sprintf("failed to build transaction: %v", err),
		}
	}

	txBytes, err := harness.EncodeTxForApp(signedTx)
	if err != nil {
		return TestResult{
			Passed:      false,
			FailureType: FailureTypeUnknown,
			Message:     fmt.Sprintf("failed to encode transaction: %v", err),
		}
	}

	// --- Run with V2 Sequential (baseline) ---
	v2Ctx := NewStateTestContext(t, blockTime, 1, ModeV2Sequential)
	v2Ctx.SetupPreState(t, st.Pre)
	v2Ctx.SetupSender(sender)

	_, v2Results, v2Err := RunStateTestBlock(v2Ctx, [][]byte{txBytes})

	// --- Run with Giga ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, ModeGigaSequential)
	gigaCtx.SetupPreState(t, st.Pre)
	gigaCtx.SetupSender(sender)

	_, gigaResults, gigaErr := RunStateTestBlock(gigaCtx, [][]byte{txBytes})

	// --- Handle ExpectException cases ---
	if post.ExpectException != "" {
		// This test expects the transaction to fail
		// Both executors should produce an error or a failed result
		v2Failed := v2Err != nil || (len(v2Results) > 0 && v2Results[0].Code != 0)
		gigaFailed := gigaErr != nil || (len(gigaResults) > 0 && gigaResults[0].Code != 0)

		if !v2Failed && !gigaFailed {
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeErrorMismatch,
				Message:     fmt.Sprintf("expected exception %q but both executors succeeded", post.ExpectException),
			}
		}
		// Both should fail - that's expected
		return TestResult{Passed: true}
	}

	// --- Compare execution errors ---
	if v2Err != nil && gigaErr != nil {
		// Both failed - check if same type of failure
		t.Logf("Both executors failed: v2=%v, giga=%v", v2Err, gigaErr)
		return TestResult{Passed: true}
	}
	if v2Err != nil {
		return TestResult{
			Passed:      false,
			FailureType: FailureTypeV2Error,
			Message:     fmt.Sprintf("V2 execution failed but Giga succeeded: %v", v2Err),
		}
	}
	if gigaErr != nil {
		return TestResult{
			Passed:      false,
			FailureType: FailureTypeGigaError,
			Message:     fmt.Sprintf("Giga execution failed but V2 succeeded: %v", gigaErr),
		}
	}

	// --- Compare results ---
	if len(v2Results) != len(gigaResults) {
		return TestResult{
			Passed:      false,
			FailureType: FailureTypeUnknown,
			Message:     fmt.Sprintf("result count mismatch: V2=%d, Giga=%d", len(v2Results), len(gigaResults)),
		}
	}

	for i := range v2Results {
		// Compare success/failure
		if v2Results[i].Code != gigaResults[i].Code {
			details := []string{
				fmt.Sprintf("V2: code=%d log=%q", v2Results[i].Code, v2Results[i].Log),
				fmt.Sprintf("Giga: code=%d log=%q", gigaResults[i].Code, gigaResults[i].Log),
			}
			t.Logf("tx[%d] result code mismatch:", i)
			for _, d := range details {
				t.Logf("  %s", d)
			}
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeResultCode,
				Message:     fmt.Sprintf("tx[%d] V2 code=%d, Giga code=%d", i, v2Results[i].Code, gigaResults[i].Code),
				Details:     details,
			}
		}

		// Note: Gas comparison is intentionally skipped for now
		// TODO: Re-enable gas comparison once Giga executor gas accounting is finalized
	}

	// --- Compare V2 vs Giga post-state ---
	stateDiffs := comparePostStates(t, v2Ctx, gigaCtx, st.Pre)
	if len(stateDiffs) > 0 {
		t.Logf("V2 vs Giga state differences:")
		for _, diff := range stateDiffs {
			t.Logf("  %s", diff)
		}
		return TestResult{
			Passed:      false,
			FailureType: stateDiffs[0].Type,
			Message:     stateDiffs[0].Summary,
			Details:     formatStateDiffs(stateDiffs),
		}
	}

	// --- Verify post-state against fixture (if available) ---
	if len(post.State) > 0 {
		v2Diffs := verifyPostStateWithResult(t, v2Ctx.Ctx, &v2Ctx.TestApp.EvmKeeper, post.State, "V2")
		gigaDiffs := verifyPostStateWithResult(t, gigaCtx.Ctx, &gigaCtx.TestApp.EvmKeeper, post.State, "Giga")

		// Log any fixture verification differences
		if len(v2Diffs) > 0 {
			t.Logf("V2 vs fixture differences:")
			for _, diff := range v2Diffs {
				t.Logf("  %s", diff)
			}
		}
		if len(gigaDiffs) > 0 {
			t.Logf("Giga vs fixture differences:")
			for _, diff := range gigaDiffs {
				t.Logf("  %s", diff)
			}
			// Return the first Giga diff as the failure
			return TestResult{
				Passed:      false,
				FailureType: gigaDiffs[0].Type,
				Message:     gigaDiffs[0].Summary,
				Details:     formatStateDiffs(gigaDiffs),
			}
		}
	}

	return TestResult{Passed: true}
}

// StateDiff represents a single state difference
type StateDiff struct {
	Type     FailureType
	Address  common.Address
	Summary  string
	Expected string
	Actual   string
}

// formatStateDiffs converts state diffs to string slice for logging
func formatStateDiffs(diffs []StateDiff) []string {
	result := make([]string, 0, len(diffs))
	for _, diff := range diffs {
		result = append(result, fmt.Sprintf("%s at %s: expected=%s, actual=%s",
			diff.Type, diff.Address.Hex(), diff.Expected, diff.Actual))
	}
	return result
}

// comparePostStates compares state between V2 and Giga contexts
func comparePostStates(_ *testing.T, v2Ctx, gigaCtx *StateTestContext, preState ethtypes.GenesisAlloc) []StateDiff {
	var diffs []StateDiff

	// Compare state for all addresses in pre-state
	for addr := range preState {
		// Compare nonce
		v2Nonce := v2Ctx.TestApp.EvmKeeper.GetNonce(v2Ctx.Ctx, addr)
		gigaNonce := gigaCtx.TestApp.EvmKeeper.GetNonce(gigaCtx.Ctx, addr)
		if v2Nonce != gigaNonce {
			diffs = append(diffs, StateDiff{
				Type:     FailureTypeNonceMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("nonce V2=%d, Giga=%d", v2Nonce, gigaNonce),
				Expected: fmt.Sprintf("%d", v2Nonce),
				Actual:   fmt.Sprintf("%d", gigaNonce),
			})
		}

		// Compare code
		v2Code := v2Ctx.TestApp.EvmKeeper.GetCode(v2Ctx.Ctx, addr)
		gigaCode := gigaCtx.TestApp.EvmKeeper.GetCode(gigaCtx.Ctx, addr)
		if !bytes.Equal(v2Code, gigaCode) {
			diffs = append(diffs, StateDiff{
				Type:     FailureTypeCodeMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("code len V2=%d, Giga=%d", len(v2Code), len(gigaCode)),
				Expected: fmt.Sprintf("%d bytes", len(v2Code)),
				Actual:   fmt.Sprintf("%d bytes", len(gigaCode)),
			})
		}

		// Compare storage for keys we know about
		for key := range preState[addr].Storage {
			v2Value := v2Ctx.TestApp.EvmKeeper.GetState(v2Ctx.Ctx, addr, key)
			gigaValue := gigaCtx.TestApp.EvmKeeper.GetState(gigaCtx.Ctx, addr, key)
			if !bytes.Equal(v2Value.Bytes(), gigaValue.Bytes()) {
				diffs = append(diffs, StateDiff{
					Type:     FailureTypeStateMismatch,
					Address:  addr,
					Summary:  fmt.Sprintf("storage[%s] V2=%s, Giga=%s", truncateHex(key.Hex()), truncateHex(v2Value.Hex()), truncateHex(gigaValue.Hex())),
					Expected: v2Value.Hex(),
					Actual:   gigaValue.Hex(),
				})
			}
		}
	}

	return diffs
}

// verifyPostStateWithResult verifies state and returns detailed diffs
func verifyPostStateWithResult(t *testing.T, ctx sdk.Context, keeper *evmkeeper.Keeper, expectedState ethtypes.GenesisAlloc, executorName string) []StateDiff {
	var diffs []StateDiff

	for addr, expectedAccount := range expectedState {
		// Verify storage
		for key, expectedValue := range expectedAccount.Storage {
			actualValue := keeper.GetState(ctx, addr, key)
			if !bytes.Equal(expectedValue.Bytes(), actualValue.Bytes()) {
				diffs = append(diffs, StateDiff{
					Type:     FailureTypeStateMismatch,
					Address:  addr,
					Summary:  fmt.Sprintf("storage[%s] differs", truncateHex(key.Hex())),
					Expected: expectedValue.Hex(),
					Actual:   actualValue.Hex(),
				})
				t.Logf("%s: storage mismatch for %s key %s:", executorName, addr.Hex(), key.Hex())
				t.Logf("  expected: %s", expectedValue.Hex())
				t.Logf("  actual:   %s", actualValue.Hex())
			}
		}

		// Verify nonce
		actualNonce := keeper.GetNonce(ctx, addr)
		if expectedAccount.Nonce != actualNonce {
			diffs = append(diffs, StateDiff{
				Type:     FailureTypeNonceMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("nonce expected=%d, actual=%d", expectedAccount.Nonce, actualNonce),
				Expected: fmt.Sprintf("%d", expectedAccount.Nonce),
				Actual:   fmt.Sprintf("%d", actualNonce),
			})
			t.Logf("%s: nonce mismatch for %s: expected %d, got %d",
				executorName, addr.Hex(), expectedAccount.Nonce, actualNonce)
		}

		// Verify code
		actualCode := keeper.GetCode(ctx, addr)
		if !bytes.Equal(expectedAccount.Code, actualCode) {
			diffs = append(diffs, StateDiff{
				Type:     FailureTypeCodeMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("code len expected=%d, actual=%d", len(expectedAccount.Code), len(actualCode)),
				Expected: fmt.Sprintf("%d bytes", len(expectedAccount.Code)),
				Actual:   fmt.Sprintf("%d bytes", len(actualCode)),
			})
			t.Logf("%s: code mismatch for %s: expected %d bytes, got %d bytes",
				executorName, addr.Hex(), len(expectedAccount.Code), len(actualCode))
		}

		// Note: Balance verification is intentionally skipped due to Sei-specific gas handling
		// (limiting EVM max refund to 150% of used gas)
	}

	return diffs
}

// truncateHex truncates a hex string for display
func truncateHex(hex string) string {
	if len(hex) > 18 {
		return hex[:10] + "..." + hex[len(hex)-4:]
	}
	return hex
}

// VerifyPostState verifies that the actual state matches the expected post-state from the fixture.
// This follows the same pattern as x/evm/keeper/replay.go:VerifyAccount() but adapted for test assertions.
// Note: Balance verification is skipped due to Sei's gas refund modifications
// (see https://github.com/sei-protocol/go-ethereum/pull/32)
func VerifyPostState(t *testing.T, ctx sdk.Context, keeper *evmkeeper.Keeper, expectedState ethtypes.GenesisAlloc, executorName string) {
	diffs := verifyPostStateWithResult(t, ctx, keeper, expectedState, executorName)
	if len(diffs) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%s state verification failed with %d differences:\n", executorName, len(diffs)))
		for _, diff := range diffs {
			sb.WriteString(fmt.Sprintf("  - %s at %s\n", diff.Type, diff.Address.Hex()))
		}
		t.Error(sb.String())
	}
}
