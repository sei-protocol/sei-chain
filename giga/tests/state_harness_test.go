package giga_test

import (
	"bytes"
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
func runStateTestComparison(t *testing.T, st *harness.StateTestJSON, post harness.StateTestPost) {
	blockTime := time.Now()

	// Build the transaction
	signedTx, sender, err := harness.BuildTransaction(st, post)
	require.NoError(t, err, "failed to build transaction")

	txBytes, err := harness.EncodeTxForApp(signedTx)
	require.NoError(t, err, "failed to encode transaction")

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
			t.Fatalf("Expected exception %q but both executors succeeded", post.ExpectException)
		}
		// Both should fail - that's expected, no further verification needed
		return
	}

	// --- Compare execution errors ---
	if v2Err != nil && gigaErr != nil {
		// Both failed - check if same type of failure
		t.Logf("Both executors failed: v2=%v, giga=%v", v2Err, gigaErr)
		return
	}
	if v2Err != nil {
		t.Fatalf("V2 execution failed but Giga succeeded: %v", v2Err)
	}
	if gigaErr != nil {
		t.Fatalf("Giga execution failed but V2 succeeded: %v", gigaErr)
	}

	// --- Compare results ---
	require.Equal(t, len(v2Results), len(gigaResults), "result count mismatch")

	for i := range v2Results {
		// Compare success/failure
		if v2Results[i].Code != gigaResults[i].Code {
			t.Logf("tx[%d] V2: code=%d log=%q", i, v2Results[i].Code, v2Results[i].Log)
			t.Logf("tx[%d] Giga: code=%d log=%q", i, gigaResults[i].Code, gigaResults[i].Log)
		}
		require.Equal(t, v2Results[i].Code, gigaResults[i].Code,
			"tx[%d] result code mismatch", i)

		// Compare EvmTxInfo if present
		if v2Results[i].EvmTxInfo != nil && gigaResults[i].EvmTxInfo != nil {
			require.Equal(t, v2Results[i].EvmTxInfo.TxHash, gigaResults[i].EvmTxInfo.TxHash,
				"tx[%d] tx hash mismatch", i)
		}
	}

	// --- Verify post-state against fixture (if available) ---
	if len(post.State) > 0 {
		VerifyPostState(t, v2Ctx.Ctx, &v2Ctx.TestApp.EvmKeeper, post.State, "V2")
		VerifyPostState(t, gigaCtx.Ctx, &gigaCtx.TestApp.EvmKeeper, post.State, "Giga")
	}
}

// VerifyPostState verifies that the actual state matches the expected post-state from the fixture.
// This follows the same pattern as x/evm/keeper/replay.go:VerifyAccount() but adapted for test assertions.
// Note: Balance verification is skipped due to Sei's gas refund modifications
// (see https://github.com/sei-protocol/go-ethereum/pull/32)
func VerifyPostState(t *testing.T, ctx sdk.Context, keeper *evmkeeper.Keeper, expectedState ethtypes.GenesisAlloc, executorName string) {
	for addr, expectedAccount := range expectedState {
		// Verify storage
		for key, expectedValue := range expectedAccount.Storage {
			actualValue := keeper.GetState(ctx, addr, key)
			if !bytes.Equal(expectedValue.Bytes(), actualValue.Bytes()) {
				t.Errorf("%s: storage mismatch for %s key %s: expected %s, got %s",
					executorName, addr.Hex(), key.Hex(), expectedValue.Hex(), actualValue.Hex())
			}
		}

		// Verify nonce
		actualNonce := keeper.GetNonce(ctx, addr)
		if expectedAccount.Nonce != actualNonce {
			t.Errorf("%s: nonce mismatch for %s: expected %d, got %d",
				executorName, addr.Hex(), expectedAccount.Nonce, actualNonce)
		}

		// Verify code
		actualCode := keeper.GetCode(ctx, addr)
		if !bytes.Equal(expectedAccount.Code, actualCode) {
			t.Errorf("%s: code mismatch for %s: expected %d bytes, got %d bytes",
				executorName, addr.Hex(), len(expectedAccount.Code), len(actualCode))
		}

		// Note: Balance verification is intentionally skipped due to Sei-specific gas handling
		// (limiting EVM max refund to 150% of used gas)
	}
}
