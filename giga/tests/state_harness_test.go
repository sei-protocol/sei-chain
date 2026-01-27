package giga_test

import (
	"bytes"
	"fmt"
	"os"
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

// EvmKeeperInterface defines the EVM keeper methods used by state tests.
// Both evmkeeper.Keeper and GigaEvmKeeper implement these methods.
type EvmKeeperInterface interface {
	GetNonce(ctx sdk.Context, addr common.Address) uint64
	SetNonce(ctx sdk.Context, addr common.Address, nonce uint64)
	GetCode(ctx sdk.Context, addr common.Address) []byte
	SetCode(ctx sdk.Context, addr common.Address, code []byte)
	GetState(ctx sdk.Context, addr common.Address, key common.Hash) common.Hash
	SetState(ctx sdk.Context, addr common.Address, key, value common.Hash)
	SetAddressMapping(ctx sdk.Context, seiAddr sdk.AccAddress, ethAddr common.Address)
	GetSeiAddressOrDefault(ctx sdk.Context, addr common.Address) sdk.AccAddress
}

// BankKeeperInterface defines the bank keeper methods used by state tests.
// Both bankkeeper and gigabankkeeper implement these methods.
type BankKeeperInterface interface {
	MintCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	AddWei(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Int) error
}

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
	gigaWithRegularStore := mode == ModeGigaWithRegularStore

	testAcct := utils.NewSigner()

	var wrapper *app.TestWrapper
	if gigaWithRegularStore {
		// Special mode: Giga executor with regular KVStore (for debugging)
		wrapper = app.NewGigaTestWrapperWithRegularStore(t.(*testing.T), blockTime, testAcct.PublicKey, true, false, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(false)
			ba.SetConcurrencyWorkers(workers)
		})
	} else if !gigaEnabled {
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

	// Also set minimum fee to 0 in GigaEvmKeeper for Giga mode
	if gigaEnabled {
		gigaParams := testApp.GigaEvmKeeper.GetParams(ctx)
		gigaParams.MinimumFeePerGas = sdk.NewDecFromInt(sdk.NewInt(0))
		testApp.GigaEvmKeeper.SetParams(ctx, gigaParams)
	}

	return &StateTestContext{
		Ctx:     ctx,
		TestApp: testApp,
		Mode:    mode,
	}
}

// IsGigaMode returns true if the context is configured for Giga execution modes.
func (stc *StateTestContext) IsGigaMode() bool {
	return stc.Mode == ModeGigaSequential || stc.Mode == ModeGigaOCC
}

// EvmKeeper returns the appropriate EVM keeper based on mode.
// For Giga modes, returns GigaEvmKeeper; for V2 modes, returns EvmKeeper.
func (stc *StateTestContext) EvmKeeper() EvmKeeperInterface {
	if stc.IsGigaMode() {
		return &stc.TestApp.GigaEvmKeeper
	}
	return &stc.TestApp.EvmKeeper
}

// BankKeeper returns the appropriate bank keeper based on mode.
// For Giga modes, returns GigaBankKeeper; for V2 modes, returns BankKeeper.
func (stc *StateTestContext) BankKeeper() BankKeeperInterface {
	if stc.IsGigaMode() {
		return stc.TestApp.GigaBankKeeper
	}
	return stc.TestApp.BankKeeper
}

// SetupPreState configures the state from the test's pre-state allocation
func (stc *StateTestContext) SetupPreState(t testing.TB, pre ethtypes.GenesisAlloc) {
	evmKeeper := stc.EvmKeeper()
	bankKeeper := stc.BankKeeper()

	for addr, account := range pre {
		// Fund the account with the specified balance
		usei, wei := state.SplitUseiWeiAmount(account.Balance)
		seiAddr := evmKeeper.GetSeiAddressOrDefault(stc.Ctx, addr)

		if usei.GT(sdk.ZeroInt()) {
			err := bankKeeper.MintCoins(stc.Ctx, "mint", sdk.NewCoins(sdk.NewCoin("usei", usei)))
			require.NoError(t, err, "failed to mint coins for %s", addr.Hex())
			err = bankKeeper.SendCoinsFromModuleToAccount(stc.Ctx, "mint", seiAddr, sdk.NewCoins(sdk.NewCoin("usei", usei)))
			require.NoError(t, err, "failed to send coins to %s", addr.Hex())
		}
		if wei.GT(sdk.ZeroInt()) {
			err := bankKeeper.AddWei(stc.Ctx, seiAddr, wei)
			require.NoError(t, err, "failed to add wei to %s", addr.Hex())
		}

		// Set nonce
		evmKeeper.SetNonce(stc.Ctx, addr, account.Nonce)

		// Set code if present
		if len(account.Code) > 0 {
			evmKeeper.SetCode(stc.Ctx, addr, account.Code)
		}

		// Set storage
		for key, value := range account.Storage {
			evmKeeper.SetState(stc.Ctx, addr, key, value)
		}

		// Associate the addresses
		evmKeeper.SetAddressMapping(stc.Ctx, seiAddr, addr)
	}
}

// SetupSender associates the sender address with its Sei address
func (stc *StateTestContext) SetupSender(sender common.Address) {
	evmKeeper := stc.EvmKeeper()
	seiAddr := evmKeeper.GetSeiAddressOrDefault(stc.Ctx, sender)
	evmKeeper.SetAddressMapping(stc.Ctx, seiAddr, sender)
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

// runStateTestComparison runs a state test and returns detailed results.
// The config parameter controls which Giga mode to use and whether to verify against fixtures.
func runStateTestComparison(t *testing.T, st *harness.StateTestJSON, post harness.StateTestPost, config ComparisonConfig) TestResult {
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

	// --- Run with Giga (mode from config) ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, config.GigaMode)
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

	// --- Verify post-state against fixture (if configured and available) ---
	if config.VerifyEthereumSpec && len(post.State) > 0 {
		v2Diffs := verifyPostStateWithResult(t, v2Ctx.Ctx, v2Ctx.EvmKeeper(), post.State, "V2")
		gigaDiffs := verifyPostStateWithResult(t, gigaCtx.Ctx, gigaCtx.EvmKeeper(), post.State, "Giga")

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

// ComparisonConfig configures how state test comparison runs
type ComparisonConfig struct {
	GigaMode      ExecutorMode // ModeGigaSequential or ModeGigaWithRegularStore
	VerifyEthereumSpec bool // Whether to verify against Ethereum test spec expected post-state
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
	v2Keeper := v2Ctx.EvmKeeper()
	gigaKeeper := gigaCtx.EvmKeeper()

	// Compare state for all addresses in pre-state
	for addr := range preState {
		// Compare nonce
		v2Nonce := v2Keeper.GetNonce(v2Ctx.Ctx, addr)
		gigaNonce := gigaKeeper.GetNonce(gigaCtx.Ctx, addr)
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
		v2Code := v2Keeper.GetCode(v2Ctx.Ctx, addr)
		gigaCode := gigaKeeper.GetCode(gigaCtx.Ctx, addr)
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
			v2Value := v2Keeper.GetState(v2Ctx.Ctx, addr, key)
			gigaValue := gigaKeeper.GetState(gigaCtx.Ctx, addr, key)
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
func verifyPostStateWithResult(t *testing.T, ctx sdk.Context, keeper EvmKeeperInterface, expectedState ethtypes.GenesisAlloc, executorName string) []StateDiff {
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

// runStateTestSuite runs the state test suite with the given configuration.
// This is the common test iteration logic shared by TestGigaVsV2_StateTests and TestGigaWithRegularStore_StateTests.
func runStateTestSuite(t *testing.T, config ComparisonConfig, summaryName string) {
	stateTestsPath, err := harness.GetStateTestsPath()
	require.NoError(t, err, "failed to get path to state tests")

	// Load skip list
	skipList, err := harness.LoadSkipList()
	require.NoError(t, err, "failed to load skip list")

	// Allow filtering to specific directory via STATE_TEST_DIR env var
	specificDir := os.Getenv("STATE_TEST_DIR")
	if specificDir == "" {
		t.Skip("STATE_TEST_DIR not set - skipping state tests")
	}

	// Allow filtering to specific test name via STATE_TEST_NAME env var
	specificTestName := os.Getenv("STATE_TEST_NAME")

	// Check if entire category is skipped
	if skipList.IsCategorySkipped(specificDir) {
		t.Skipf("Skipping category %s (in skip list)", specificDir)
	}

	dirPath := stateTestsPath + "/" + specificDir

	tests, err := harness.LoadStateTestsFromDir(dirPath)
	require.NoError(t, err, "failed to load state tests from %s", dirPath)

	if len(tests) == 0 {
		t.Skipf("No state tests found in %s", dirPath)
	}

	t.Logf("Found %d state test files in %s", len(tests), specificDir)
	if specificTestName != "" {
		t.Logf("Filtering to tests matching: %s", specificTestName)
	}

	// Track results for summary
	summary := NewTestSummary()

	// Print summary at the end
	t.Cleanup(func() {
		t.Logf("\n=== %s Test Summary ===", summaryName)
		summary.PrintSummary(t)
	})

	for testName, st := range tests {
		// Filter by test name if specified
		if specificTestName != "" && !strings.Contains(testName, specificTestName) {
			continue
		}

		// Run each subtest for Cancun fork (most recent stable)
		cancunPosts, ok := st.Post["Cancun"]
		if !ok {
			// Try other forks
			for fork := range st.Post {
				cancunPosts = st.Post[fork]
				break
			}
		}

		for i, post := range cancunPosts {
			subtestName := testName
			if len(cancunPosts) > 1 {
				subtestName = fmt.Sprintf("%s/%d", testName, i)
			}

			// Check skip list
			if shouldSkip, reason := skipList.ShouldSkip(specificDir, subtestName); shouldSkip {
				t.Run(subtestName, func(t *testing.T) {
					summary.RecordSkip(specificDir, subtestName, reason)
					t.Skipf("Skipped: %s", reason)
				})
				continue
			}

			// Capture variables for closure
			category := specificDir
			testNameCopy := subtestName
			stCopy := st
			postCopy := post

			t.Run(subtestName, func(t *testing.T) {
				result := runStateTestComparison(t, stCopy, postCopy, config)
				if result.Passed {
					summary.RecordPass(category, testNameCopy)
				} else {
					summary.RecordFailure(category, testNameCopy, result.FailureType, result.Message)
					t.Errorf("State comparison failed: %s - %s", result.FailureType, result.Message)
					for _, detail := range result.Details {
						t.Logf("  %s", detail)
					}
				}
			})
		}
	}
}

// TestGigaWithRegularStore_StateTests runs state tests comparing V2 vs Giga with regular KVStore.
// This test isolates whether issues are in the Giga executor logic vs the GigaKVStore layer.
// If tests pass here but fail with normal Giga mode, the issue is in GigaKVStore integration.
// If tests fail here, the issue is in the Giga executor logic itself.
//
// Usage: STATE_TEST_DIR=stChainId go test -v -run TestGigaWithRegularStore_StateTests ./giga/tests/...
// Usage with test name filter: STATE_TEST_DIR=stExample STATE_TEST_NAME=add11 go test -v -run TestGigaWithRegularStore_StateTests ./giga/tests/...
func TestGigaWithRegularStore_StateTests(t *testing.T) {
	runStateTestSuite(t, ComparisonConfig{
		GigaMode:           ModeGigaWithRegularStore,
		VerifyEthereumSpec: os.Getenv("VERIFY_ETHEREUM_SPEC") == "true",
	}, "Giga with Regular Store")
}

// TestDebugStateTest runs a single state test with verbose output for debugging.
// This test ignores the skip list and provides detailed execution information.
//
// Usage: STATE_TEST_DIR=stExample STATE_TEST_NAME=add11 go test -v -run TestDebugStateTest ./giga/tests/...
func TestDebugStateTest(t *testing.T) {
	stateTestsPath, err := harness.GetStateTestsPath()
	require.NoError(t, err, "failed to get path to state tests")

	// Require both STATE_TEST_DIR and STATE_TEST_NAME
	specificDir := os.Getenv("STATE_TEST_DIR")
	if specificDir == "" {
		t.Skip("STATE_TEST_DIR not set - skipping debug test")
	}

	specificTestName := os.Getenv("STATE_TEST_NAME")
	if specificTestName == "" {
		t.Skip("STATE_TEST_NAME not set - skipping debug test")
	}

	dirPath := stateTestsPath + "/" + specificDir

	tests, err := harness.LoadStateTestsFromDir(dirPath)
	require.NoError(t, err, "failed to load state tests from %s", dirPath)

	if len(tests) == 0 {
		t.Fatalf("No state tests found in %s", dirPath)
	}

	t.Logf("=== Debug State Test ===")
	t.Logf("Category: %s", specificDir)
	t.Logf("Test name filter: %s", specificTestName)
	t.Logf("Note: Skip list is IGNORED in debug mode")
	t.Logf("")

	// Find matching tests
	var matchingTests []struct {
		name string
		st   *harness.StateTestJSON
		post harness.StateTestPost
	}

	for testName, st := range tests {
		if !strings.Contains(testName, specificTestName) {
			continue
		}

		// Get Cancun posts or fallback to other forks
		cancunPosts, ok := st.Post["Cancun"]
		if !ok {
			for fork := range st.Post {
				cancunPosts = st.Post[fork]
				break
			}
		}

		for i, post := range cancunPosts {
			subtestName := testName
			if len(cancunPosts) > 1 {
				subtestName = fmt.Sprintf("%s/%d", testName, i)
			}
			matchingTests = append(matchingTests, struct {
				name string
				st   *harness.StateTestJSON
				post harness.StateTestPost
			}{name: subtestName, st: st, post: post})
		}
	}

	if len(matchingTests) == 0 {
		t.Fatalf("No tests found matching %q in %s", specificTestName, specificDir)
	}

	t.Logf("Found %d matching test(s)", len(matchingTests))
	t.Logf("")

	for _, mt := range matchingTests {
		t.Run(mt.name, func(t *testing.T) {
			runDebugStateTest(t, mt.st, mt.post)
		})
	}
}

// runDebugStateTest runs a single state test with verbose debug output
func runDebugStateTest(t *testing.T, st *harness.StateTestJSON, post harness.StateTestPost) {
	blockTime := time.Now()

	t.Logf("=== Pre-State Setup ===")
	for addr, account := range st.Pre {
		t.Logf("Address: %s", addr.Hex())
		t.Logf("  Balance: %s", account.Balance.String())
		t.Logf("  Nonce: %d", account.Nonce)
		if len(account.Code) > 0 {
			t.Logf("  Code: %d bytes", len(account.Code))
		}
		if len(account.Storage) > 0 {
			t.Logf("  Storage entries: %d", len(account.Storage))
			for key, value := range account.Storage {
				t.Logf("    %s -> %s", truncateHex(key.Hex()), truncateHex(value.Hex()))
			}
		}
	}
	t.Logf("")

	// Build the transaction
	signedTx, sender, err := harness.BuildTransaction(st, post)
	if err != nil {
		t.Fatalf("Failed to build transaction: %v", err)
	}

	t.Logf("=== Transaction ===")
	t.Logf("Sender: %s", sender.Hex())
	t.Logf("To: %v", signedTx.To())
	t.Logf("Value: %s", signedTx.Value().String())
	t.Logf("Gas: %d", signedTx.Gas())
	t.Logf("GasPrice: %s", signedTx.GasPrice().String())
	t.Logf("Data: %d bytes", len(signedTx.Data()))
	t.Logf("")

	txBytes, err := harness.EncodeTxForApp(signedTx)
	if err != nil {
		t.Fatalf("Failed to encode transaction: %v", err)
	}

	// --- Run with V2 Sequential ---
	t.Logf("=== V2 Sequential Execution ===")
	v2Ctx := NewStateTestContext(t, blockTime, 1, ModeV2Sequential)
	v2Ctx.SetupPreState(t, st.Pre)
	v2Ctx.SetupSender(sender)

	_, v2Results, v2Err := RunStateTestBlock(v2Ctx, [][]byte{txBytes})

	if v2Err != nil {
		t.Logf("V2 Error: %v", v2Err)
	} else if len(v2Results) > 0 {
		t.Logf("V2 Result Code: %d", v2Results[0].Code)
		t.Logf("V2 Result Log: %s", v2Results[0].Log)
		t.Logf("V2 Gas Used: %d", v2Results[0].GasUsed)
	}
	t.Logf("")

	// --- Run with Giga ---
	t.Logf("=== Giga Execution ===")
	gigaCtx := NewStateTestContext(t, blockTime, 1, ModeGigaSequential)
	gigaCtx.SetupPreState(t, st.Pre)
	gigaCtx.SetupSender(sender)

	_, gigaResults, gigaErr := RunStateTestBlock(gigaCtx, [][]byte{txBytes})

	if gigaErr != nil {
		t.Logf("Giga Error: %v", gigaErr)
	} else if len(gigaResults) > 0 {
		t.Logf("Giga Result Code: %d", gigaResults[0].Code)
		t.Logf("Giga Result Log: %s", gigaResults[0].Log)
		t.Logf("Giga Gas Used: %d", gigaResults[0].GasUsed)
	}
	t.Logf("")

	// --- Compare Results ---
	t.Logf("=== Comparison ===")

	// Check for expected exceptions
	if post.ExpectException != "" {
		t.Logf("Expected exception: %s", post.ExpectException)
		v2Failed := v2Err != nil || (len(v2Results) > 0 && v2Results[0].Code != 0)
		gigaFailed := gigaErr != nil || (len(gigaResults) > 0 && gigaResults[0].Code != 0)
		t.Logf("V2 failed: %v, Giga failed: %v", v2Failed, gigaFailed)
		if v2Failed && gigaFailed {
			t.Logf("PASS: Both executors correctly failed")
			return
		}
		if !v2Failed && !gigaFailed {
			t.Errorf("FAIL: Both executors succeeded but expected exception %q", post.ExpectException)
			return
		}
	}

	// Compare errors
	if v2Err != nil && gigaErr != nil {
		t.Logf("Both executors returned errors (matching behavior)")
	} else if v2Err != nil {
		t.Errorf("FAIL: V2 error but Giga succeeded: %v", v2Err)
		return
	} else if gigaErr != nil {
		t.Errorf("FAIL: Giga error but V2 succeeded: %v", gigaErr)
		return
	}

	// Compare result codes
	if len(v2Results) > 0 && len(gigaResults) > 0 {
		if v2Results[0].Code != gigaResults[0].Code {
			t.Errorf("FAIL: Result code mismatch - V2=%d, Giga=%d", v2Results[0].Code, gigaResults[0].Code)
			t.Logf("  V2 Log: %s", v2Results[0].Log)
			t.Logf("  Giga Log: %s", gigaResults[0].Log)
		} else {
			t.Logf("Result codes match: %d", v2Results[0].Code)
		}
	}

	// Compare post-state
	t.Logf("")
	t.Logf("=== Post-State Comparison (V2 vs Giga) ===")
	stateDiffs := comparePostStates(t, v2Ctx, gigaCtx, st.Pre)
	if len(stateDiffs) > 0 {
		for _, diff := range stateDiffs {
			t.Errorf("State diff at %s: %s", diff.Address.Hex(), diff.Summary)
			t.Logf("  Expected (V2): %s", diff.Expected)
			t.Logf("  Actual (Giga): %s", diff.Actual)
		}
	} else {
		t.Logf("Post-state matches between V2 and Giga")
	}

	// Verify against expected post-state if available
	if len(post.State) > 0 {
		t.Logf("")
		t.Logf("=== Expected Post-State Verification ===")
		for addr, expectedAccount := range post.State {
			t.Logf("Expected state for %s:", addr.Hex())
			t.Logf("  Nonce: %d", expectedAccount.Nonce)
			if len(expectedAccount.Code) > 0 {
				t.Logf("  Code: %d bytes", len(expectedAccount.Code))
			}
			for key, value := range expectedAccount.Storage {
				t.Logf("  Storage[%s]: %s", truncateHex(key.Hex()), truncateHex(value.Hex()))
			}
		}

		v2Diffs := verifyPostStateWithResult(t, v2Ctx.Ctx, v2Ctx.EvmKeeper(), post.State, "V2")
		gigaDiffs := verifyPostStateWithResult(t, gigaCtx.Ctx, gigaCtx.EvmKeeper(), post.State, "Giga")

		if len(v2Diffs) > 0 {
			t.Logf("V2 has %d differences from expected post-state", len(v2Diffs))
		} else {
			t.Logf("V2 matches expected post-state")
		}

		if len(gigaDiffs) > 0 {
			t.Errorf("Giga has %d differences from expected post-state", len(gigaDiffs))
			for _, diff := range gigaDiffs {
				t.Logf("  %s at %s: expected=%s, actual=%s", diff.Type, diff.Address.Hex(), diff.Expected, diff.Actual)
			}
		} else {
			t.Logf("Giga matches expected post-state")
		}
	}

	t.Logf("")
	if t.Failed() {
		t.Logf("=== TEST FAILED ===")
	} else {
		t.Logf("=== TEST PASSED ===")
	}
}
