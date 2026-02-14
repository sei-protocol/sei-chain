package giga_test

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"strconv"
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
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
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
	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
}

// FailureType categorizes the type of test failure
type FailureType string

const (
	FailureTypeResultCode       FailureType = "result_code"
	FailureTypeGasMismatch      FailureType = "gas_mismatch"
	FailureTypeStateMismatch    FailureType = "state_mismatch"
	FailureTypeCodeMismatch     FailureType = "code_mismatch"
	FailureTypeNonceMismatch    FailureType = "nonce_mismatch"
	FailureTypeBalanceMismatch  FailureType = "balance_mismatch"
	FailureTypeErrorMismatch    FailureType = "error_mismatch"
	FailureTypeV2Error          FailureType = "v2_error"
	FailureTypeGigaError        FailureType = "giga_error"
	FailureTypeFailedTxBehavior FailureType = "failed_tx_behavior" // Known issue: V2 and Giga differ in error code/gas for failed txs
	FailureTypeUnknown          FailureType = "unknown"
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

// EvmKeeper returns the EVM keeper.
// After PR #2780, both Giga and V2 executors share the same underlying data layer,
// so we always use the regular EvmKeeper for pre/post state setup.
func (stc *StateTestContext) EvmKeeper() EvmKeeperInterface {
	return &stc.TestApp.EvmKeeper
}

// BankKeeper returns the bank keeper.
// After PR #2780, both Giga and V2 executors share the same underlying data layer,
// so we always use the regular BankKeeper for pre/post state setup.
func (stc *StateTestContext) BankKeeper() BankKeeperInterface {
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

// SetupEnv configures the block environment from the test's env settings.
// This sets the base fee and block gas limit to match the test's expected block conditions.
func (stc *StateTestContext) SetupEnv(env harness.StateTestEnv) {
	// Set base fee from test environment
	if env.BaseFee != "" {
		baseFee := harness.ParseHexBig(env.BaseFee)
		baseFeeSDK := sdk.NewDecFromBigInt(baseFee)

		// Set the next base fee in EvmKeeper
		// This is what GetBaseFee() actually reads, not params.BaseFeePerGas
		// After PR #2780, both Giga and V2 executors share the same underlying data layer,
		// so we only need to set it once via EvmKeeper - both paths will read the same value.
		stc.TestApp.EvmKeeper.SetNextBaseFeePerGas(stc.Ctx, baseFeeSDK)
	}

	// Set block gas limit from test environment
	// This is critical for tests like lowGasLimit.json where TX gas > block gas should fail
	if env.GasLimit != "" {
		gasLimit := harness.ParseHexBig(env.GasLimit).Int64()
		cp := stc.Ctx.ConsensusParams()
		if cp == nil {
			cp = &tmproto.ConsensusParams{}
		}
		if cp.Block == nil {
			cp.Block = &tmproto.BlockParams{}
		}
		cp.Block.MaxGas = gasLimit
		stc.Ctx = stc.Ctx.WithConsensusParams(cp)
	}
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
	v2Ctx.SetupEnv(st.Env)
	v2Ctx.SetupPreState(t, st.Pre)
	v2Ctx.SetupSender(sender)

	_, v2Results, v2Err := RunStateTestBlock(v2Ctx, [][]byte{txBytes})

	// --- Run with Giga (mode from config) ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, config.GigaMode)
	gigaCtx.SetupEnv(st.Env)
	gigaCtx.SetupPreState(t, st.Pre)
	gigaCtx.SetupSender(sender)

	_, gigaResults, gigaErr := RunStateTestBlock(gigaCtx, [][]byte{txBytes})

	// Debug: log result codes and gas used
	if len(v2Results) > 0 && len(gigaResults) > 0 {
		t.Logf("V2 result:   code=%d gas=%d log=%q", v2Results[0].Code, v2Results[0].GasUsed, v2Results[0].Log)
		t.Logf("Giga result: code=%d gas=%d log=%q", gigaResults[0].Code, gigaResults[0].GasUsed, gigaResults[0].Log)
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

		// Compare gas used
		if v2Results[i].GasUsed != gigaResults[i].GasUsed {
			return TestResult{
				Passed:      false,
				FailureType: FailureTypeGasMismatch,
				Message:     fmt.Sprintf("tx[%d] gas mismatch: V2=%d, Giga=%d", i, v2Results[i].GasUsed, gigaResults[i].GasUsed),
				Details: []string{
					fmt.Sprintf("V2 gas used: %d", v2Results[i].GasUsed),
					fmt.Sprintf("Giga gas used: %d", gigaResults[i].GasUsed),
				},
			}
		}
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

	// --- Verify against Ethereum spec (if configured) ---
	if config.VerifyEthereumSpec {
		// Check ExpectException if set
		if post.ExpectException != "" {
			v2Failed := v2Err != nil || (len(v2Results) > 0 && v2Results[0].Code != 0)
			gigaFailed := gigaErr != nil || (len(gigaResults) > 0 && gigaResults[0].Code != 0)

			if !v2Failed || !gigaFailed {
				return TestResult{
					Passed:      false,
					FailureType: FailureTypeErrorMismatch,
					Message:     fmt.Sprintf("expected exception %q but V2 failed=%v, Giga failed=%v", post.ExpectException, v2Failed, gigaFailed),
				}
			}
			// Both failed as expected by spec
			return TestResult{Passed: true}
		}

		// Verify post-state against fixture
		if len(post.State) > 0 {
			v2Diffs := verifyPostStateWithResult(t, v2Ctx.Ctx, v2Ctx.EvmKeeper(), v2Ctx.BankKeeper(), post.State, "V2")
			gigaDiffs := verifyPostStateWithResult(t, gigaCtx.Ctx, gigaCtx.EvmKeeper(), gigaCtx.BankKeeper(), post.State, "Giga")

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
	GigaMode           ExecutorMode // ModeGigaSequential or ModeGigaWithRegularStore
	VerifyEthereumSpec bool         // Whether to verify against Ethereum test spec expected post-state
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
func comparePostStates(t *testing.T, v2Ctx, gigaCtx *StateTestContext, preState ethtypes.GenesisAlloc) []StateDiff {
	var diffs []StateDiff
	v2Keeper := v2Ctx.EvmKeeper()
	gigaKeeper := gigaCtx.EvmKeeper()
	v2Bank := v2Ctx.BankKeeper()
	gigaBank := gigaCtx.BankKeeper()

	// Compare state for all addresses in pre-state
	for addr := range preState {
		// Get Sei address from EVM address
		seiAddr := v2Keeper.GetSeiAddressOrDefault(v2Ctx.Ctx, addr)

		// Compare balance (usei + wei)
		// Total balance = usei × 10^12 + wei
		v2Usei := v2Bank.GetBalance(v2Ctx.Ctx, seiAddr, "usei").Amount
		v2Wei := v2Bank.GetWeiBalance(v2Ctx.Ctx, seiAddr)
		gigaUsei := gigaBank.GetBalance(gigaCtx.Ctx, seiAddr, "usei").Amount
		gigaWei := gigaBank.GetWeiBalance(gigaCtx.Ctx, seiAddr)

		// Calculate total balance in wei for comparison
		weiPerUsei := new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil) // 10^12
		v2Total := new(big.Int).Mul(v2Usei.BigInt(), weiPerUsei)
		v2Total.Add(v2Total, v2Wei.BigInt())
		gigaTotal := new(big.Int).Mul(gigaUsei.BigInt(), weiPerUsei)
		gigaTotal.Add(gigaTotal, gigaWei.BigInt())

		if v2Total.Cmp(gigaTotal) != 0 {
			// Log detailed balance breakdown for debugging
			t.Logf("Balance mismatch for %s:", addr.Hex())
			t.Logf("  V2:   usei=%s wei=%s (total=%s)", v2Usei.String(), v2Wei.String(), v2Total.String())
			t.Logf("  Giga: usei=%s wei=%s (total=%s)", gigaUsei.String(), gigaWei.String(), gigaTotal.String())
			diff := new(big.Int).Sub(v2Total, gigaTotal)
			t.Logf("  Diff (V2-Giga): %s wei", diff.String())

			diffs = append(diffs, StateDiff{
				Type:     FailureTypeBalanceMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("balance V2(usei=%s,wei=%s) != Giga(usei=%s,wei=%s)", v2Usei.String(), v2Wei.String(), gigaUsei.String(), gigaWei.String()),
				Expected: fmt.Sprintf("total=%s (usei=%s, wei=%s)", v2Total.String(), v2Usei.String(), v2Wei.String()),
				Actual:   fmt.Sprintf("total=%s (usei=%s, wei=%s)", gigaTotal.String(), gigaUsei.String(), gigaWei.String()),
			})
		}

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
func verifyPostStateWithResult(t *testing.T, ctx sdk.Context, keeper EvmKeeperInterface, bankKeeper BankKeeperInterface, expectedState ethtypes.GenesisAlloc, executorName string) []StateDiff {
	var diffs []StateDiff

	for addr, expectedAccount := range expectedState {
		// Verify balance
		seiAddr := keeper.GetSeiAddressOrDefault(ctx, addr)
		actualCoin := bankKeeper.GetBalance(ctx, seiAddr, "usei")
		actualBalance := actualCoin.Amount.BigInt()

		expectedBalance := expectedAccount.Balance
		if expectedBalance == nil {
			expectedBalance = big.NewInt(0)
		}

		if actualBalance.Cmp(expectedBalance) != 0 {
			diffs = append(diffs, StateDiff{
				Type:     FailureTypeBalanceMismatch,
				Address:  addr,
				Summary:  fmt.Sprintf("balance expected=%s, actual=%s", expectedBalance.String(), actualBalance.String()),
				Expected: expectedBalance.String(),
				Actual:   actualBalance.String(),
			})
			t.Logf("%s: balance mismatch for %s: expected %s, got %s",
				executorName, addr.Hex(), expectedBalance.String(), actualBalance.String())
		}

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

	// Allow filtering to specific test index via STATE_TEST_INDEX env var
	specificIndexStr := os.Getenv("STATE_TEST_INDEX")
	specificIndex := -1
	if specificIndexStr != "" {
		var parseErr error
		specificIndex, parseErr = strconv.Atoi(specificIndexStr)
		if parseErr != nil {
			t.Fatalf("Invalid STATE_TEST_INDEX: %s", specificIndexStr)
		}
	}

	// Allow bypassing skip list for analysis purposes
	ignoreSkipList := os.Getenv("IGNORE_SKIP_LIST") == "true"

	// Check if entire category is skipped
	if !ignoreSkipList && skipList.IsCategorySkipped(specificDir) {
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
			// Filter by specific index if specified
			if specificIndex >= 0 && i != specificIndex {
				continue
			}

			subtestName := testName
			if len(cancunPosts) > 1 {
				subtestName = fmt.Sprintf("%s/%d", testName, i)
			}

			// Check skip list (unless bypassed for analysis)
			if !ignoreSkipList {
				if shouldSkip, reason := skipList.ShouldSkip(specificDir, subtestName); shouldSkip {
					t.Run(subtestName, func(t *testing.T) {
						summary.RecordSkip(specificDir, subtestName, reason)
						t.Skipf("Skipped: %s", reason)
					})
					continue
				}
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
