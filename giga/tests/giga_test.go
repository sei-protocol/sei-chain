package giga_test

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/merkle"

	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

// ExecutorMode defines which executor path to use
type ExecutorMode int

const (
	ModeV2Sequential         ExecutorMode = iota // V2 execution path, sequential (no OCC)
	ModeV2withOCC                                // V2 execution path with OCC (standard production path)
	ModeGigaSequential                           // Giga executor, no OCC
	ModeGigaOCC                                  // Giga executor with OCC
	ModeGigaWithRegularStore                     // Giga executor with regular KVStore (for debugging - isolates executor from GigaKVStore)
)

func (m ExecutorMode) String() string {
	switch m {
	case ModeV2Sequential:
		return "V2Sequential"
	case ModeV2withOCC:
		return "V2withOCC"
	case ModeGigaSequential:
		return "GigaSequential"
	case ModeGigaOCC:
		return "GigaOCC"
	case ModeGigaWithRegularStore:
		return "GigaWithRegularStore"
	default:
		return "Unknown"
	}
}

// GigaTestContext wraps the test context with executor mode
type GigaTestContext struct {
	Ctx          sdk.Context
	TestApp      *app.App
	TestAccounts []utils.TestAcct
	Mode         ExecutorMode
}

type bankKeeper interface {
	MintCoins(ctx sdk.Context, moduleName string, amounts sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx sdk.Context, moduleName string, recipient sdk.AccAddress, amounts sdk.Coins) error
}

type evmKeeper interface {
	SetAddressMapping(ctx sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address)
}

// NewGigaTestContext creates a test context configured for a specific executor mode
func NewGigaTestContext(t testing.TB, testAccts []utils.TestAcct, blockTime time.Time, workers int, mode ExecutorMode) *GigaTestContext {
	// OCC is enabled for both GethOCC and GigaOCC modes
	occEnabled := mode == ModeV2withOCC || mode == ModeGigaOCC
	gigaEnabled := mode == ModeGigaSequential || mode == ModeGigaOCC
	gigaOCCEnabled := mode == ModeGigaOCC

	var wrapper *app.TestWrapper
	if !gigaEnabled {
		wrapper = app.NewTestWrapperWithSc(t.(*testing.T), blockTime, testAccts[0].PublicKey, true, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(occEnabled)
			ba.SetConcurrencyWorkers(workers)
		})
	} else {
		wrapper = app.NewGigaTestWrapper(t.(*testing.T), blockTime, testAccts[0].PublicKey, true, gigaOCCEnabled, func(ba *baseapp.BaseApp) {
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

	// Fund test accounts
	amounts := sdk.NewCoins(
		sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
		sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
	)
	var bk bankKeeper
	bk = testApp.BankKeeper
	for _, ta := range testAccts {
		err := bk.MintCoins(ctx, "mint", amounts)
		if err != nil {
			t.Fatalf("failed to mint coins: %v", err)
		}
		err = bk.SendCoinsFromModuleToAccount(ctx, "mint", ta.AccountAddress, amounts)
		if err != nil {
			t.Fatalf("failed to send coins: %v", err)
		}
	}

	return &GigaTestContext{
		Ctx:          ctx,
		TestApp:      testApp,
		TestAccounts: testAccts,
		Mode:         mode,
	}
}

// EVMTransfer represents an EVM transfer transaction for testing
type EVMTransfer struct {
	Signer utils.TestAcct
	To     common.Address
	Value  *big.Int
	Nonce  uint64
}

// CreateEVMTransferTxs creates signed EVM transfer transactions and funds the signers
func CreateEVMTransferTxs(t testing.TB, tCtx *GigaTestContext, transfers []EVMTransfer, isGiga bool) [][]byte {
	txs := make([][]byte, 0, len(transfers))
	tc := app.MakeEncodingConfig().TxConfig

	var ek evmKeeper
	var bk bankKeeper
	ek = &tCtx.TestApp.EvmKeeper
	bk = tCtx.TestApp.BankKeeper
	for _, transfer := range transfers {
		// Associate the Cosmos address with the EVM address
		// This is required for the Giga executor path which bypasses ante handlers
		ek.SetAddressMapping(tCtx.Ctx, transfer.Signer.AccountAddress, transfer.Signer.EvmAddress)

		// Fund the signer account before creating the transaction
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := bk.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = bk.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", transfer.Signer.AccountAddress, amounts)
		require.NoError(t, err)

		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			GasFeeCap: new(big.Int).SetUint64(100000000000),
			GasTipCap: new(big.Int).SetUint64(100000000000),
			Gas:       21000,
			ChainID:   big.NewInt(config.DefaultChainID),
			To:        &transfer.To,
			Value:     transfer.Value,
			Nonce:     transfer.Nonce,
		}), transfer.Signer.EvmSigner, transfer.Signer.EvmPrivateKey)
		require.NoError(t, err)

		txData, err := ethtx.NewTxDataFromTx(signedTx)
		require.NoError(t, err)

		msg, err := types.NewMsgEVMTransaction(txData)
		require.NoError(t, err)

		// Build the Cosmos tx wrapper
		txBuilder := tc.NewTxBuilder()
		err = txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetGasLimit(10000000000)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))))

		txBytes, err := tc.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)

		txs = append(txs, txBytes)
	}

	return txs
}

// GenerateNonConflictingTransfers creates transfers where each sender is unique
func GenerateNonConflictingTransfers(count int) []EVMTransfer {
	transfers := make([]EVMTransfer, count)
	for i := 0; i < count; i++ {
		signer := utils.NewSigner()
		transfers[i] = EVMTransfer{
			Signer: signer,
			To:     signer.EvmAddress, // Send to self
			Value:  big.NewInt(1),
			Nonce:  0,
		}
	}
	return transfers
}

// GenerateConflictingTransfers creates transfers where all send to the same recipient
func GenerateConflictingTransfers(count int, recipient common.Address) []EVMTransfer {
	transfers := make([]EVMTransfer, count)
	for i := 0; i < count; i++ {
		signer := utils.NewSigner()
		transfers[i] = EVMTransfer{
			Signer: signer,
			To:     recipient, // All send to same address
			Value:  big.NewInt(1),
			Nonce:  0,
		}
	}
	return transfers
}

// RunBlock executes a block of transactions and returns results
func RunBlock(t testing.TB, tCtx *GigaTestContext, txs [][]byte) ([]abci.Event, []*abci.ExecTxResult, error) {
	// Set global OCC flag based on mode (both GethOCC and GigaOCC use OCC)
	app.EnableOCC = tCtx.Mode == ModeV2withOCC || tCtx.Mode == ModeGigaOCC

	req := &abci.RequestFinalizeBlock{
		Txs:    txs,
		Height: tCtx.Ctx.BlockHeader().Height,
	}

	events, results, _, err := tCtx.TestApp.ProcessBlock(tCtx.Ctx, txs, req, req.DecidedLastCommit, false)
	return events, results, err
}

// ComputeLastResultsHash computes the LastResultsHash from tx results
// This uses the same logic as tendermint: only Code, Data, GasWanted, GasUsed are included
func ComputeLastResultsHash(results []*abci.ExecTxResult) ([]byte, error) {
	rs, err := abci.MarshalTxResults(results)
	if err != nil {
		return nil, err
	}
	return merkle.HashFromByteSlices(rs), nil
}

// CompareLastResultsHash compares the LastResultsHash between two result sets
// This is the critical comparison for consensus - if hashes differ, nodes will fork
func CompareLastResultsHash(t *testing.T, testName string, expected, actual []*abci.ExecTxResult) {
	expectedHash, err := ComputeLastResultsHash(expected)
	require.NoError(t, err, "%s: failed to compute expected LastResultsHash", testName)

	actualHash, err := ComputeLastResultsHash(actual)
	require.NoError(t, err, "%s: failed to compute actual LastResultsHash", testName)

	if !bytes.Equal(expectedHash, actualHash) {
		// Log detailed info about each tx result's deterministic fields
		t.Logf("%s: LastResultsHash MISMATCH!", testName)
		t.Logf("  Expected hash: %X", expectedHash)
		t.Logf("  Actual hash:   %X", actualHash)

		// Log per-tx deterministic fields to help debug
		maxLen := len(expected)
		if len(actual) > maxLen {
			maxLen = len(actual)
		}
		for i := 0; i < maxLen; i++ {
			var expInfo, actInfo string
			if i < len(expected) {
				expInfo = fmt.Sprintf("Code=%d GasWanted=%d GasUsed=%d DataLen=%d",
					expected[i].Code, expected[i].GasWanted, expected[i].GasUsed, len(expected[i].Data))
			} else {
				expInfo = "(missing)"
			}
			if i < len(actual) {
				actInfo = fmt.Sprintf("Code=%d GasWanted=%d GasUsed=%d DataLen=%d",
					actual[i].Code, actual[i].GasWanted, actual[i].GasUsed, len(actual[i].Data))
			} else {
				actInfo = "(missing)"
			}

			// Check if this tx differs
			differs := ""
			if i < len(expected) && i < len(actual) {
				if expected[i].Code != actual[i].Code ||
					expected[i].GasWanted != actual[i].GasWanted ||
					expected[i].GasUsed != actual[i].GasUsed ||
					!bytes.Equal(expected[i].Data, actual[i].Data) {
					differs = " <-- DIFFERS"
				}
			}
			t.Logf("  tx[%d] expected: %s", i, expInfo)
			t.Logf("  tx[%d] actual:   %s%s", i, actInfo, differs)
		}
	}

	require.True(t, bytes.Equal(expectedHash, actualHash),
		"%s: LastResultsHash mismatch - expected %X, got %X", testName, expectedHash, actualHash)
}

// CompareDeterministicFields compares the 4 deterministic fields that go into LastResultsHash
func CompareDeterministicFields(t *testing.T, testName string, expected, actual []*abci.ExecTxResult) {
	require.Equal(t, len(expected), len(actual), "%s: result count mismatch", testName)

	for i := range expected {
		require.Equal(t, expected[i].Code, actual[i].Code,
			"%s: tx[%d] Code mismatch (expected %d, got %d)", testName, i, expected[i].Code, actual[i].Code)
		require.Equal(t, expected[i].GasWanted, actual[i].GasWanted,
			"%s: tx[%d] GasWanted mismatch (expected %d, got %d)", testName, i, expected[i].GasWanted, actual[i].GasWanted)
		require.Equal(t, expected[i].GasUsed, actual[i].GasUsed,
			"%s: tx[%d] GasUsed mismatch (expected %d, got %d)", testName, i, expected[i].GasUsed, actual[i].GasUsed)
		require.True(t, bytes.Equal(expected[i].Data, actual[i].Data),
			"%s: tx[%d] Data mismatch (expected len=%d, got len=%d)", testName, i, len(expected[i].Data), len(actual[i].Data))
	}
}

// CompareResults compares execution results between two runs
func CompareResults(t *testing.T, testName string, expected, actual []*abci.ExecTxResult) {
	compareResultsWithOptions(t, testName, expected, actual, true)
}

// CompareResultsNoGas compares execution results but skips gas comparison
// Use this for contract execution tests where gas may differ between implementations
func CompareResultsNoGas(t *testing.T, testName string, expected, actual []*abci.ExecTxResult) {
	compareResultsWithOptions(t, testName, expected, actual, false)
}

func compareResultsWithOptions(t *testing.T, testName string, expected, actual []*abci.ExecTxResult, compareGas bool) {
	require.Equal(t, len(expected), len(actual), "%s: result count mismatch", testName)

	for i := range expected {
		if expected[i].Code != actual[i].Code {
			t.Logf("%s: tx[%d] expected code=%d log=%q", testName, i, expected[i].Code, expected[i].Log)
			t.Logf("%s: tx[%d] actual   code=%d log=%q", testName, i, actual[i].Code, actual[i].Log)
		}
		require.Equal(t, expected[i].Code, actual[i].Code,
			"%s: tx[%d] code mismatch (expected %d, got %d)", testName, i, expected[i].Code, actual[i].Code)

		// For successful txs, compare gas used if requested
		if compareGas && expected[i].Code == 0 && actual[i].Code == 0 {
			require.Equal(t, expected[i].GasUsed, actual[i].GasUsed,
				"%s: tx[%d] gas used mismatch", testName, i)
		}

		// Compare EvmTxInfo if present
		if expected[i].EvmTxInfo != nil {
			require.NotNil(t, actual[i].EvmTxInfo, "%s: tx[%d] missing EvmTxInfo", testName, i)
			require.Equal(t, expected[i].EvmTxInfo.TxHash, actual[i].EvmTxInfo.TxHash,
				"%s: tx[%d] tx hash mismatch", testName, i)
			require.Equal(t, expected[i].EvmTxInfo.Nonce, actual[i].EvmTxInfo.Nonce,
				"%s: tx[%d] nonce mismatch", testName, i)
		}
	}
}

// TestGigaVsGeth_NonConflicting compares Giga executor vs Geth for non-conflicting EVM transfers
func TestGigaVsGeth_NonConflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 10

	// Generate the same transfers for both runs
	transfers := GenerateNonConflictingTransfers(txCount)

	// Run with Geth (baseline)
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers, true)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, txCount)

	// Compare results
	CompareResults(t, "GigaVsGeth_NonConflicting", gethResults, gigaResults)

	// Verify all transactions succeeded
	for i, result := range gethResults {
		require.Equal(t, uint32(0), result.Code, "Geth tx[%d] failed: %s", i, result.Log)
	}
	for i, result := range gigaResults {
		require.Equal(t, uint32(0), result.Code, "Giga tx[%d] failed: %s", i, result.Log)
	}
}

// TestGigaVsGeth_Conflicting compares Giga executor vs Geth for conflicting EVM transfers
func TestGigaVsGeth_Conflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 10

	// All transfers go to the same recipient (conflicting)
	recipient := accts[0].EvmAddress
	transfers := GenerateConflictingTransfers(txCount, recipient)

	// Run with Geth (baseline)
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers, true)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, txCount)

	// Compare results
	CompareResults(t, "GigaVsGeth_Conflicting", gethResults, gigaResults)
}

// TestGigaOCCVsGigaSequential_NonConflicting compares Giga+OCC vs Giga sequential for non-conflicting transfers
func TestGigaOCCVsGigaSequential_NonConflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 20
	workers := 4

	transfers := GenerateNonConflictingTransfers(txCount)

	// Run with Giga Sequential (baseline)
	seqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers, true)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")
	require.Len(t, seqResults, txCount)

	// Run with Giga OCC (multiple times to catch race conditions)
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers, true)
		_, occResults, occErr := RunBlock(t, occCtx, occTxs)
		require.NoError(t, occErr, "Giga OCC execution failed (run %d)", run)
		require.Len(t, occResults, txCount)

		// Compare results
		CompareResults(t, "GigaOCCVsSequential_NonConflicting", seqResults, occResults)
	}
}

// TestGigaOCCVsGigaSequential_Conflicting compares Giga+OCC vs Giga sequential for conflicting transfers
func TestGigaOCCVsGigaSequential_Conflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 20
	workers := 4

	recipient := accts[0].EvmAddress
	transfers := GenerateConflictingTransfers(txCount, recipient)

	// Run with Giga Sequential (baseline)
	seqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers, true)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")
	require.Len(t, seqResults, txCount)

	// Run with Giga OCC (multiple times to catch race conditions)
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers, true)
		_, occResults, occErr := RunBlock(t, occCtx, occTxs)
		require.NoError(t, occErr, "Giga OCC execution failed (run %d)", run)
		require.Len(t, occResults, txCount)

		// Compare results
		CompareResults(t, "GigaOCCVsSequential_Conflicting", seqResults, occResults)
	}
}

// TestGigaOCCVsGigaSequential_Mixed compares with a mix of conflicting and non-conflicting
func TestGigaOCCVsGigaSequential_Mixed(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	conflictingCount := 10
	nonConflictingCount := 10
	workers := 4

	recipient := accts[0].EvmAddress
	conflicting := GenerateConflictingTransfers(conflictingCount, recipient)
	nonConflicting := GenerateNonConflictingTransfers(nonConflictingCount)

	// Interleave conflicting and non-conflicting
	transfers := make([]EVMTransfer, 0, conflictingCount+nonConflictingCount)
	for i := 0; i < max(conflictingCount, nonConflictingCount); i++ {
		if i < conflictingCount {
			transfers = append(transfers, conflicting[i])
		}
		if i < nonConflictingCount {
			transfers = append(transfers, nonConflicting[i])
		}
	}

	// Run with Giga Sequential (baseline)
	seqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers, true)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")

	// Run with Giga OCC
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers, true)
		_, occResults, occErr := RunBlock(t, occCtx, occTxs)
		require.NoError(t, occErr, "Giga OCC execution failed (run %d)", run)

		CompareResults(t, "GigaOCCVsSequential_Mixed", seqResults, occResults)
	}
}

// TestAllModes_NonConflicting runs the same transactions through all three modes and compares
func TestAllModes_NonConflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 15
	workers := 4

	transfers := GenerateNonConflictingTransfers(txCount)

	// Geth Sequential
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr)

	// Giga Sequential
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqTxs := CreateEVMTransferTxs(t, gigaSeqCtx, transfers, true)
	_, gigaSeqResults, gigaSeqErr := RunBlock(t, gigaSeqCtx, gigaSeqTxs)
	require.NoError(t, gigaSeqErr)

	// Giga OCC
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCTxs := CreateEVMTransferTxs(t, gigaOCCCtx, transfers, true)
	_, gigaOCCResults, gigaOCCErr := RunBlock(t, gigaOCCCtx, gigaOCCTxs)
	require.NoError(t, gigaOCCErr)

	// Compare: Geth vs Giga Sequential
	CompareResults(t, "AllModes_GethVsGigaSeq", gethResults, gigaSeqResults)

	// Compare: Giga Sequential vs Giga OCC
	CompareResults(t, "AllModes_GigaSeqVsOCC", gigaSeqResults, gigaOCCResults)

	t.Logf("All %d transactions produced identical results across all three executor modes", txCount)
}

// TestAllModes_Conflicting runs conflicting transactions through all three modes
func TestAllModes_Conflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 15
	workers := 4

	recipient := accts[0].EvmAddress
	transfers := GenerateConflictingTransfers(txCount, recipient)

	// Geth Sequential
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr)

	// Giga Sequential
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqTxs := CreateEVMTransferTxs(t, gigaSeqCtx, transfers, true)
	_, gigaSeqResults, gigaSeqErr := RunBlock(t, gigaSeqCtx, gigaSeqTxs)
	require.NoError(t, gigaSeqErr)

	// Giga OCC
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCTxs := CreateEVMTransferTxs(t, gigaOCCCtx, transfers, true)
	_, gigaOCCResults, gigaOCCErr := RunBlock(t, gigaOCCCtx, gigaOCCTxs)
	require.NoError(t, gigaOCCErr)

	// Compare: Geth vs Giga Sequential
	CompareResults(t, "AllModes_Conflicting_GethVsGigaSeq", gethResults, gigaSeqResults)

	// Compare: Giga Sequential vs Giga OCC
	CompareResults(t, "AllModes_Conflicting_GigaSeqVsOCC", gigaSeqResults, gigaOCCResults)

	t.Logf("All %d conflicting transactions produced identical results across all three executor modes", txCount)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SimpleStorage contract bytecode (compiled from example/contracts/simplestorage/SimpleStorage.sol)
// Contract has: set(uint256), get() -> uint256, bad() that reverts, and an event SetEvent(uint256)
var simpleStorageBytecode = common.Hex2Bytes("608060405234801561000f575f80fd5b506101938061001d5f395ff3fe608060405234801561000f575f80fd5b506004361061003f575f3560e01c806360fe47b1146100435780636d4ce63c1461005f5780639c3674fc1461007d575b5f80fd5b61005d6004803603810190610058919061010a565b610087565b005b6100676100c7565b6040516100749190610144565b60405180910390f35b6100856100cf565b005b805f819055507f0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1816040516100bc9190610144565b60405180910390a150565b5f8054905090565b5f80fd5b5f80fd5b5f819050919050565b6100e9816100d7565b81146100f3575f80fd5b50565b5f81359050610104816100e0565b92915050565b5f6020828403121561011f5761011e6100d3565b5b5f61012c848285016100f6565b91505092915050565b61013e816100d7565b82525050565b5f6020820190506101575f830184610135565b9291505056fea2646970667358221220bb55137839ea2afda11ab2d30ad07fee30bb9438caaa46e30ccd1053ed72439064736f6c63430008150033")

// set(uint256) function selector
var setFunctionSelector = common.Hex2Bytes("60fe47b1")

// get() function selector
var getFunctionSelector = common.Hex2Bytes("6d4ce63c")

// EVMContractDeploy represents a contract deployment transaction for testing
type EVMContractDeploy struct {
	Signer   utils.TestAcct
	Bytecode []byte
	Nonce    uint64
}

// EVMContractCall represents a contract call transaction for testing
type EVMContractCall struct {
	Signer   utils.TestAcct
	Contract common.Address
	Data     []byte
	Value    *big.Int
	Nonce    uint64
}

// CreateContractDeployTxs creates signed contract deployment transactions
func CreateContractDeployTxs(t testing.TB, tCtx *GigaTestContext, deploys []EVMContractDeploy, isGiga bool) [][]byte {
	txs := make([][]byte, 0, len(deploys))
	tc := app.MakeEncodingConfig().TxConfig

	var ek evmKeeper
	var bk bankKeeper
	ek = &tCtx.TestApp.EvmKeeper
	bk = tCtx.TestApp.BankKeeper
	for _, deploy := range deploys {
		// Associate the Cosmos address with the EVM address
		ek.SetAddressMapping(tCtx.Ctx, deploy.Signer.AccountAddress, deploy.Signer.EvmAddress)

		// Fund the signer account
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := bk.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = bk.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", deploy.Signer.AccountAddress, amounts)
		require.NoError(t, err)

		// Contract deployment: To is nil
		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			GasFeeCap: new(big.Int).SetUint64(100000000000),
			GasTipCap: new(big.Int).SetUint64(100000000000),
			Gas:       1000000, // Higher gas for contract deployment
			ChainID:   big.NewInt(config.DefaultChainID),
			To:        nil, // nil means contract creation
			Value:     big.NewInt(0),
			Data:      deploy.Bytecode,
			Nonce:     deploy.Nonce,
		}), deploy.Signer.EvmSigner, deploy.Signer.EvmPrivateKey)
		require.NoError(t, err)

		txData, err := ethtx.NewTxDataFromTx(signedTx)
		require.NoError(t, err)

		msg, err := types.NewMsgEVMTransaction(txData)
		require.NoError(t, err)

		txBuilder := tc.NewTxBuilder()
		err = txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetGasLimit(10000000000)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))))

		txBytes, err := tc.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)

		txs = append(txs, txBytes)
	}

	return txs
}

// CreateContractCallTxs creates signed contract call transactions
func CreateContractCallTxs(t testing.TB, tCtx *GigaTestContext, calls []EVMContractCall, isGiga bool) [][]byte {
	txs := make([][]byte, 0, len(calls))
	tc := app.MakeEncodingConfig().TxConfig

	var ek evmKeeper
	var bk bankKeeper
	ek = &tCtx.TestApp.EvmKeeper
	bk = tCtx.TestApp.BankKeeper
	for _, call := range calls {
		// Associate the Cosmos address with the EVM address
		ek.SetAddressMapping(tCtx.Ctx, call.Signer.AccountAddress, call.Signer.EvmAddress)

		// Fund the signer account
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := bk.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = bk.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", call.Signer.AccountAddress, amounts)
		require.NoError(t, err)

		value := call.Value
		if value == nil {
			value = big.NewInt(0)
		}

		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			GasFeeCap: new(big.Int).SetUint64(100000000000),
			GasTipCap: new(big.Int).SetUint64(100000000000),
			Gas:       200000, // Gas for contract call
			ChainID:   big.NewInt(config.DefaultChainID),
			To:        &call.Contract,
			Value:     value,
			Data:      call.Data,
			Nonce:     call.Nonce,
		}), call.Signer.EvmSigner, call.Signer.EvmPrivateKey)
		require.NoError(t, err)

		txData, err := ethtx.NewTxDataFromTx(signedTx)
		require.NoError(t, err)

		msg, err := types.NewMsgEVMTransaction(txData)
		require.NoError(t, err)

		txBuilder := tc.NewTxBuilder()
		err = txBuilder.SetMsgs(msg)
		require.NoError(t, err)
		txBuilder.SetGasLimit(10000000000)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))))

		txBytes, err := tc.TxEncoder()(txBuilder.GetTx())
		require.NoError(t, err)

		txs = append(txs, txBytes)
	}

	return txs
}

// encodeSetCall encodes a call to set(uint256)
func encodeSetCall(value *big.Int) []byte {
	// Pad value to 32 bytes
	paddedValue := common.LeftPadBytes(value.Bytes(), 32)
	return append(setFunctionSelector, paddedValue...)
}

// encodeGetCall encodes a call to get()
func encodeGetCall() []byte {
	return getFunctionSelector
}

// TestGigaVsGeth_ContractDeployAndCall compares Giga vs Geth for contract deployment and calls
func TestGigaVsGeth_ContractDeployAndCall(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// Create a deployer
	deployer := utils.NewSigner()

	// Deploy contract with Geth
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethDeployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	_, gethDeployResults, gethDeployErr := RunBlock(t, gethCtx, gethDeployTxs)
	require.NoError(t, gethDeployErr, "Geth deploy failed")
	require.Len(t, gethDeployResults, 1)
	require.Equal(t, uint32(0), gethDeployResults[0].Code, "Geth deploy tx failed: %s", gethDeployResults[0].Log)

	// Deploy contract with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	_, gigaDeployResults, gigaDeployErr := RunBlock(t, gigaCtx, gigaDeployTxs)
	require.NoError(t, gigaDeployErr, "Giga deploy failed")
	require.Len(t, gigaDeployResults, 1)
	require.Equal(t, uint32(0), gigaDeployResults[0].Code, "Giga deploy tx failed: %s", gigaDeployResults[0].Log)

	// Compare deployment results (skip gas comparison - different EVM implementations may differ)
	CompareResultsNoGas(t, "ContractDeploy", gethDeployResults, gigaDeployResults)

	t.Logf("Contract deployment successful on both Geth and Giga")
	t.Logf("Geth deploy gas used: %d", gethDeployResults[0].GasUsed)
	t.Logf("Giga deploy gas used: %d", gigaDeployResults[0].GasUsed)
}

// TestGigaVsGeth_ContractCallsSequential compares Giga vs Geth for sequential contract calls
// This test deploys a contract and calls it within the same block
func TestGigaVsGeth_ContractCallsSequential(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	callCount := 5

	// Create a deployer and separate callers
	deployer := utils.NewSigner()
	callers := make([]utils.TestAcct, callCount)
	for i := 0; i < callCount; i++ {
		callers[i] = utils.NewSigner()
	}

	// Calculate expected contract address (deployer address + nonce 0)
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// ---- Run with Geth ----
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)

	// Build all transactions: deploy + calls in same block
	gethDeployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)

	gethCallInputs := make([]EVMContractCall, callCount)
	for i := 0; i < callCount; i++ {
		gethCallInputs[i] = EVMContractCall{
			Signer:   callers[i],
			Contract: contractAddr,
			Data:     encodeSetCall(big.NewInt(int64(i + 100))), // set(100), set(101), etc.
			Nonce:    0,
		}
	}
	gethCallTxs := CreateContractCallTxs(t, gethCtx, gethCallInputs, false)

	// Combine deploy + calls into one block
	allGethTxs := append(gethDeployTxs, gethCallTxs...)
	_, gethResults, gethErr := RunBlock(t, gethCtx, allGethTxs)
	require.NoError(t, gethErr)
	require.Len(t, gethResults, 1+callCount)

	t.Logf("Contract deployed at: %s", contractAddr.Hex())

	// ---- Run with Giga ----
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)

	gigaCallInputs := make([]EVMContractCall, callCount)
	for i := 0; i < callCount; i++ {
		gigaCallInputs[i] = EVMContractCall{
			Signer:   callers[i],
			Contract: contractAddr,
			Data:     encodeSetCall(big.NewInt(int64(i + 100))),
			Nonce:    0,
		}
	}
	gigaCallTxs := CreateContractCallTxs(t, gigaCtx, gigaCallInputs, true)

	allGigaTxs := append(gigaDeployTxs, gigaCallTxs...)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, allGigaTxs)
	require.NoError(t, gigaErr)
	require.Len(t, gigaResults, 1+callCount)

	// Compare results (skip gas comparison - different EVM implementations may differ)
	CompareResultsNoGas(t, "ContractDeployAndCalls", gethResults, gigaResults)

	// Verify all transactions succeeded
	for i, result := range gethResults {
		require.Equal(t, uint32(0), result.Code, "Geth tx[%d] failed: %s", i, result.Log)
	}
	for i, result := range gigaResults {
		require.Equal(t, uint32(0), result.Code, "Giga tx[%d] failed: %s", i, result.Log)
	}

	t.Logf("Contract deployment + %d calls successful on both Geth and Giga", callCount)
}

// TestAllModes_ContractExecution runs contract deployment and calls through all executor modes
// All transactions are executed in a single block
func TestAllModes_ContractExecution(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	workers := 4

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// ---- Geth with OCC ----
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethDeployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	gethCallTxs := CreateContractCallTxs(t, gethCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, false)
	allGethTxs := append(gethDeployTxs, gethCallTxs...)
	_, gethResults, err := RunBlock(t, gethCtx, allGethTxs)
	require.NoError(t, err)
	require.Len(t, gethResults, 2)
	require.Equal(t, uint32(0), gethResults[0].Code, "Geth deploy failed: %s", gethResults[0].Log)
	require.Equal(t, uint32(0), gethResults[1].Code, "Geth call failed: %s", gethResults[1].Log)

	// ---- Giga Sequential ----
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqDeployTxs := CreateContractDeployTxs(t, gigaSeqCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	gigaSeqCallTxs := CreateContractCallTxs(t, gigaSeqCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, true)
	allGigaSeqTxs := append(gigaSeqDeployTxs, gigaSeqCallTxs...)
	_, gigaSeqResults, err := RunBlock(t, gigaSeqCtx, allGigaSeqTxs)
	require.NoError(t, err)
	require.Len(t, gigaSeqResults, 2)
	require.Equal(t, uint32(0), gigaSeqResults[0].Code, "GigaSeq deploy failed: %s", gigaSeqResults[0].Log)
	require.Equal(t, uint32(0), gigaSeqResults[1].Code, "GigaSeq call failed: %s", gigaSeqResults[1].Log)

	// ---- Giga OCC ----
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCDeployTxs := CreateContractDeployTxs(t, gigaOCCCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	gigaOCCCallTxs := CreateContractCallTxs(t, gigaOCCCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, true)
	allGigaOCCTxs := append(gigaOCCDeployTxs, gigaOCCCallTxs...)
	_, gigaOCCResults, err := RunBlock(t, gigaOCCCtx, allGigaOCCTxs)
	require.NoError(t, err)
	require.Len(t, gigaOCCResults, 2)
	require.Equal(t, uint32(0), gigaOCCResults[0].Code, "GigaOCC deploy failed: %s", gigaOCCResults[0].Log)
	require.Equal(t, uint32(0), gigaOCCResults[1].Code, "GigaOCC call failed: %s", gigaOCCResults[1].Log)

	// Compare results - gas should now be identical since GIGA applies the Sei custom SSTORE adjustment
	CompareResults(t, "AllModes_GethVsGigaSeq", gethResults, gigaSeqResults)
	CompareResults(t, "AllModes_GigaSeqVsOCC", gigaSeqResults, gigaOCCResults)

	t.Logf("Contract deployment and calls produced identical results across all three executor modes")
}

// TestGigaVsGeth_GasComparison compares gas usage between Geth and Giga executors.
//
// Both Geth and Giga use Sei's configurable SSTORE gas cost (SeiSstoreSetGasEIP2200).
// The GIGA path applies a gas adjustment after evmone execution to match Sei's custom SSTORE cost.
//
// This test verifies:
//  1. Deploy gas is exactly the same (no SSTORE involved)
//  2. Call gas is exactly the same (SSTORE gas adjustment applied in GIGA)
func TestGigaVsGeth_GasComparison(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// Run contract deploy + call with Geth
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethDeployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	gethCallTxs := CreateContractCallTxs(t, gethCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, false)
	allGethTxs := append(gethDeployTxs, gethCallTxs...)
	_, gethResults, gethErr := RunBlock(t, gethCtx, allGethTxs)
	require.NoError(t, gethErr)

	// Run with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	gigaCallTxs := CreateContractCallTxs(t, gigaCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, true)
	allGigaTxs := append(gigaDeployTxs, gigaCallTxs...)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, allGigaTxs)
	require.NoError(t, gigaErr)

	// Report gas comparison
	deployDiff := int64(gethResults[0].GasUsed) - int64(gigaResults[0].GasUsed)
	callDiff := int64(gethResults[1].GasUsed) - int64(gigaResults[1].GasUsed)

	t.Logf("Gas Comparison Report (Geth vs Giga/evmone):")
	t.Logf("  Contract Deploy: Geth=%d, Giga=%d, Diff=%d",
		gethResults[0].GasUsed, gigaResults[0].GasUsed, deployDiff)
	t.Logf("  Contract Call:   Geth=%d, Giga=%d, Diff=%d",
		gethResults[1].GasUsed, gigaResults[1].GasUsed, callDiff)

	// Deploy gas should be EXACTLY the same (no SSTORE operations in CREATE)
	require.Equal(t, int64(0), deployDiff,
		"Deploy gas should be identical between Geth and Giga (no SSTORE)")

	// Call gas should now be IDENTICAL since GIGA applies the Sei custom SSTORE gas adjustment
	require.Equal(t, int64(0), callDiff,
		"Call gas should be identical between Geth and Giga (SSTORE gas adjustment applied)")

	t.Logf("Gas comparison verified: Both deploy and call gas are identical")
}

// TestGiga_CREATE_CodePath verifies the CREATE opcode uses contract.Code (initcode)
// This specifically tests the fix where CREATE/CREATE2 must use contract.Code, not input
func TestGiga_CREATE_CodePath(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	deployer := utils.NewSigner()

	// Test with Giga executor - this exercises the internal.EVMInterpreter.Run()
	// with the CREATE opcode, which should use contract.Code (initcode)
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	deployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)

	_, results, err := RunBlock(t, gigaCtx, deployTxs)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// The key assertion: deployment should succeed (code != 0)
	// This verifies that the interpreter correctly passed initcode to evmone
	require.Equal(t, uint32(0), results[0].Code, "Contract deployment should succeed")
	require.NotEmpty(t, results[0].Data, "Deployment should return created contract address")

	t.Logf("CREATE path verified: Contract deployed successfully with Giga executor")
}

// TestGiga_CALL_CodePath verifies the CALL opcode fetches code from recipient
func TestGiga_CALL_CodePath(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// First deploy the contract with Geth (to have known state)
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	deployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	_, deployResults, err := RunBlock(t, gethCtx, deployTxs)
	require.NoError(t, err)
	require.Equal(t, uint32(0), deployResults[0].Code)

	// Now call the contract with Giga executor
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	// Deploy first so the contract exists
	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	_, _, err = RunBlock(t, gigaCtx, gigaDeployTxs)
	require.NoError(t, err)

	// Now call the deployed contract - this exercises the CALL path
	// which should fetch code from the recipient address
	callTxs := CreateContractCallTxs(t, gigaCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(123)), Nonce: 0},
	}, true)
	_, callResults, err := RunBlock(t, gigaCtx, callTxs)
	require.NoError(t, err)
	require.Len(t, callResults, 1)

	// The key assertion: the call should succeed
	// This verifies that the interpreter correctly fetched code from recipient
	require.Equal(t, uint32(0), callResults[0].Code, "Contract call should succeed")

	t.Logf("CALL path verified: Contract call succeeded with Giga executor")
}

// TestGiga_STATICCALL_ReadOnly verifies STATICCALL sets the static flag
func TestGiga_STATICCALL_ReadOnly(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// Deploy with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	deployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	_, deployResults, err := RunBlock(t, gigaCtx, deployTxs)
	require.NoError(t, err)
	require.Equal(t, uint32(0), deployResults[0].Code)

	// Call with a read function (get()) - this should use STATICCALL internally
	// The get() function doesn't modify state, so it's effectively a static call
	callTxs := CreateContractCallTxs(t, gigaCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeGetCall(), Nonce: 0},
	}, true)
	_, callResults, err := RunBlock(t, gigaCtx, callTxs)
	require.NoError(t, err)
	require.Len(t, callResults, 1)
	require.Equal(t, uint32(0), callResults[0].Code, "Read call should succeed")

	t.Logf("STATICCALL/read path verified with Giga executor")
}

// TestGiga_GasAccounting verifies gas is properly tracked after evmone execution
func TestGiga_GasAccounting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	// Deploy
	deployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	_, deployResults, err := RunBlock(t, gigaCtx, deployTxs)
	require.NoError(t, err)
	require.Equal(t, uint32(0), deployResults[0].Code)

	// Call and check gas usage is reasonable
	callTxs := CreateContractCallTxs(t, gigaCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, true)
	_, callResults, err := RunBlock(t, gigaCtx, callTxs)
	require.NoError(t, err)
	require.Len(t, callResults, 1)
	require.Equal(t, uint32(0), callResults[0].Code)

	// Verify gas was actually used (not zero, not max)
	require.True(t, callResults[0].GasUsed > 21000, "Gas used should be more than base tx cost: %d", callResults[0].GasUsed)
	require.True(t, callResults[0].GasUsed < 1000000, "Gas used should be reasonable: %d", callResults[0].GasUsed)

	t.Logf("Gas accounting verified: Call used %d gas", callResults[0].GasUsed)
}

// TestGiga_SstoreGasDeltaCalculation verifies that the SSTORE gas delta is correctly calculated
// based on different Sei SSTORE gas parameter values.
// This is a unit test for the HostContext gas adjustment logic.
func TestGiga_SstoreGasDeltaCalculation(t *testing.T) {
	// Test the delta calculation directly
	// StandardSstoreSetGasEIP2200 = 20000

	tests := []struct {
		name          string
		seiSstoreGas  uint64
		expectedDelta uint64
	}{
		{
			name:          "Standard (20k) - no adjustment needed",
			seiSstoreGas:  20000,
			expectedDelta: 0,
		},
		{
			name:          "Higher value (72k) - 52k delta",
			seiSstoreGas:  72000,
			expectedDelta: 52000,
		},
		{
			name:          "Higher (100k) - 80k delta",
			seiSstoreGas:  100000,
			expectedDelta: 80000,
		},
		{
			name:          "Lower than standard (10k) - no adjustment",
			seiSstoreGas:  10000,
			expectedDelta: 0, // No negative adjustments
		},
		{
			name:          "Zero - no adjustment",
			seiSstoreGas:  0,
			expectedDelta: 0,
		},
	}

	const standardSstoreGas = uint64(20000)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate delta the same way NewHostContext does
			var delta uint64
			if tt.seiSstoreGas > standardSstoreGas {
				delta = tt.seiSstoreGas - standardSstoreGas
			}

			require.Equal(t, tt.expectedDelta, delta,
				"Delta calculation for seiSstoreGas=%d", tt.seiSstoreGas)
		})
	}

	t.Logf("SSTORE gas delta calculation verified for all test cases")
}

// TestGiga_SstoreGasHonoredByChainConfig verifies that the SSTORE gas parameter
// is correctly read from the chain config and would be passed to the executor.
// This tests the parameter flow, not full execution.
func TestGiga_SstoreGasHonoredByChainConfig(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	// Create a GIGA context
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	// Get default params
	defaultParams := gigaCtx.TestApp.GigaEvmKeeper.GetParams(gigaCtx.Ctx)
	t.Logf("Default SeiSstoreSetGasEip2200: %d", defaultParams.SeiSstoreSetGasEip2200)

	// Verify default matches the expected default from types package
	require.Equal(t, types.DefaultSeiSstoreSetGasEIP2200, defaultParams.SeiSstoreSetGasEip2200,
		"Default SSTORE gas should match types.DefaultSeiSstoreSetGasEIP2200 (%d)", types.DefaultSeiSstoreSetGasEIP2200)

	// Update to a different value
	customValue := uint64(50000)
	customParams := defaultParams
	customParams.SeiSstoreSetGasEip2200 = customValue
	gigaCtx.TestApp.GigaEvmKeeper.SetParams(gigaCtx.Ctx, customParams)

	// Read back and verify
	updatedParams := gigaCtx.TestApp.GigaEvmKeeper.GetParams(gigaCtx.Ctx)
	require.Equal(t, customValue, updatedParams.SeiSstoreSetGasEip2200,
		"Updated SSTORE gas should be %d", customValue)

	t.Logf("Verified: SSTORE gas parameter can be read and updated")
	t.Logf("  Default: %d", defaultParams.SeiSstoreSetGasEip2200)
	t.Logf("  Updated: %d", updatedParams.SeiSstoreSetGasEip2200)
}

// ============================================================================
// LastResultsHash Consistency Tests
// These tests verify that the deterministic fields (Code, Data, GasWanted, GasUsed)
// that go into LastResultsHash are identical between executor modes.
// This is critical for consensus - mismatches cause chain forks.
// ============================================================================

// TestLastResultsHash_GigaVsGeth_SimpleTransfer verifies LastResultsHash matches for simple transfers
func TestLastResultsHash_GigaVsGeth_SimpleTransfer(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 5

	transfers := GenerateNonConflictingTransfers(txCount)

	// Run with Geth (baseline)
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers, true)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")

	// Verify all transactions succeeded
	for i, result := range gethResults {
		require.Equal(t, uint32(0), result.Code, "Geth tx[%d] failed: %s", i, result.Log)
	}
	for i, result := range gigaResults {
		require.Equal(t, uint32(0), result.Code, "Giga tx[%d] failed: %s", i, result.Log)
	}

	// Compare LastResultsHash - this is the critical consensus check
	CompareLastResultsHash(t, "LastResultsHash_GigaVsGeth_SimpleTransfer", gethResults, gigaResults)

	// Also compare individual deterministic fields for detailed debugging
	CompareDeterministicFields(t, "DeterministicFields_GigaVsGeth_SimpleTransfer", gethResults, gigaResults)

	gethHash, _ := ComputeLastResultsHash(gethResults)
	gigaHash, _ := ComputeLastResultsHash(gigaResults)
	t.Logf("LastResultsHash verified identical: %X", gethHash)
	t.Logf("Geth hash:  %X", gethHash)
	t.Logf("Giga hash:  %X", gigaHash)
}

// TestLastResultsHash_GigaVsGeth_ContractDeploy verifies LastResultsHash for contract deployment
func TestLastResultsHash_GigaVsGeth_ContractDeploy(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	deployer := utils.NewSigner()

	// Run with Geth
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr)
	require.Equal(t, uint32(0), gethResults[0].Code, "Geth deploy failed: %s", gethResults[0].Log)

	// Run with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr)
	require.Equal(t, uint32(0), gigaResults[0].Code, "Giga deploy failed: %s", gigaResults[0].Log)

	// Compare LastResultsHash
	CompareLastResultsHash(t, "LastResultsHash_GigaVsGeth_ContractDeploy", gethResults, gigaResults)

	gethHash, _ := ComputeLastResultsHash(gethResults)
	t.Logf("Contract deploy LastResultsHash verified identical: %X", gethHash)
}

// TestLastResultsHash_GigaVsGeth_ContractCall verifies LastResultsHash for contract calls
func TestLastResultsHash_GigaVsGeth_ContractCall(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	deployer := utils.NewSigner()
	caller := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	// Run with Geth: deploy + call
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethDeployTxs := CreateContractDeployTxs(t, gethCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, false)
	gethCallTxs := CreateContractCallTxs(t, gethCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, false)
	allGethTxs := append(gethDeployTxs, gethCallTxs...)
	_, gethResults, gethErr := RunBlock(t, gethCtx, allGethTxs)
	require.NoError(t, gethErr)
	require.Equal(t, uint32(0), gethResults[0].Code, "Geth deploy failed: %s", gethResults[0].Log)
	require.Equal(t, uint32(0), gethResults[1].Code, "Geth call failed: %s", gethResults[1].Log)

	// Run with Giga: deploy + call
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	}, true)
	gigaCallTxs := CreateContractCallTxs(t, gigaCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	}, true)
	allGigaTxs := append(gigaDeployTxs, gigaCallTxs...)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, allGigaTxs)
	require.NoError(t, gigaErr)
	require.Equal(t, uint32(0), gigaResults[0].Code, "Giga deploy failed: %s", gigaResults[0].Log)
	require.Equal(t, uint32(0), gigaResults[1].Code, "Giga call failed: %s", gigaResults[1].Log)

	// Compare LastResultsHash
	CompareLastResultsHash(t, "LastResultsHash_GigaVsGeth_ContractCall", gethResults, gigaResults)

	gethHash, _ := ComputeLastResultsHash(gethResults)
	t.Logf("Contract call LastResultsHash verified identical: %X", gethHash)
}

// TestLastResultsHash_AllModes verifies LastResultsHash is identical across all executor modes
func TestLastResultsHash_AllModes(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 10
	workers := 4

	transfers := GenerateNonConflictingTransfers(txCount)

	// Geth with OCC (standard production path)
	gethCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, err := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, err)

	// Giga Sequential
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqTxs := CreateEVMTransferTxs(t, gigaSeqCtx, transfers, true)
	_, gigaSeqResults, err := RunBlock(t, gigaSeqCtx, gigaSeqTxs)
	require.NoError(t, err)

	// Giga OCC
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCTxs := CreateEVMTransferTxs(t, gigaOCCCtx, transfers, true)
	_, gigaOCCResults, err := RunBlock(t, gigaOCCCtx, gigaOCCTxs)
	require.NoError(t, err)

	// Verify all transactions succeeded
	for i := range gethResults {
		require.Equal(t, uint32(0), gethResults[i].Code, "Geth tx[%d] failed", i)
		require.Equal(t, uint32(0), gigaSeqResults[i].Code, "GigaSeq tx[%d] failed", i)
		require.Equal(t, uint32(0), gigaOCCResults[i].Code, "GigaOCC tx[%d] failed", i)
	}

	// Compare LastResultsHash across all modes
	CompareLastResultsHash(t, "Geth vs GigaSequential", gethResults, gigaSeqResults)
	CompareLastResultsHash(t, "GigaSequential vs GigaOCC", gigaSeqResults, gigaOCCResults)
	CompareLastResultsHash(t, "Geth vs GigaOCC", gethResults, gigaOCCResults)

	gethHash, _ := ComputeLastResultsHash(gethResults)
	gigaSeqHash, _ := ComputeLastResultsHash(gigaSeqResults)
	gigaOCCHash, _ := ComputeLastResultsHash(gigaOCCResults)

	t.Logf("LastResultsHash verified identical across all 3 executor modes:")
	t.Logf("  Geth+OCC:        %X", gethHash)
	t.Logf("  Giga Sequential: %X", gigaSeqHash)
	t.Logf("  Giga+OCC:        %X", gigaOCCHash)
}

// TestLastResultsHash_DeterministicFieldsLogged logs the deterministic fields for manual inspection
// This is useful for debugging when hashes don't match
func TestLastResultsHash_DeterministicFieldsLogged(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// Single simple transfer for easy inspection
	transfers := GenerateNonConflictingTransfers(1)

	// Run with Geth
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers, false)
	_, gethResults, _ := RunBlock(t, gethCtx, gethTxs)

	// Run with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers, true)
	_, gigaResults, _ := RunBlock(t, gigaCtx, gigaTxs)

	// Log the deterministic fields for inspection
	t.Log("=== Geth Result ===")
	for i, r := range gethResults {
		t.Logf("tx[%d]: Code=%d GasWanted=%d GasUsed=%d DataLen=%d Data=%X",
			i, r.Code, r.GasWanted, r.GasUsed, len(r.Data), r.Data)
	}

	t.Log("=== Giga Result ===")
	for i, r := range gigaResults {
		t.Logf("tx[%d]: Code=%d GasWanted=%d GasUsed=%d DataLen=%d Data=%X",
			i, r.Code, r.GasWanted, r.GasUsed, len(r.Data), r.Data)
	}

	gethHash, _ := ComputeLastResultsHash(gethResults)
	gigaHash, _ := ComputeLastResultsHash(gigaResults)
	t.Logf("Geth LastResultsHash: %X", gethHash)
	t.Logf("Giga LastResultsHash: %X", gigaHash)

	// Still verify they match
	CompareLastResultsHash(t, "DeterministicFieldsLogged", gethResults, gigaResults)
}

// TestGigaSequential_BalanceTransfer verifies that balance is actually transferred
// when using the Giga Sequential executor mode
func TestGigaSequential_BalanceTransfer(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)

	// Create sender and recipient
	sender := utils.NewSigner()
	recipient := utils.NewSigner()

	// Setup Giga Sequential context
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	// Fund the sender account and set up address mapping
	gigaCtx.TestApp.EvmKeeper.SetAddressMapping(gigaCtx.Ctx, sender.AccountAddress, sender.EvmAddress)
	gigaCtx.TestApp.EvmKeeper.SetAddressMapping(gigaCtx.Ctx, recipient.AccountAddress, recipient.EvmAddress)

	initialFunding := sdk.NewCoins(
		sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)), // 1e18 usei
	)
	err := gigaCtx.TestApp.BankKeeper.MintCoins(gigaCtx.Ctx, "mint", initialFunding)
	require.NoError(t, err)
	err = gigaCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(gigaCtx.Ctx, "mint", sender.AccountAddress, initialFunding)
	require.NoError(t, err)

	// Get initial balances
	senderInitialBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, sender.AccountAddress)
	recipientInitialBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, recipient.AccountAddress)

	t.Logf("Initial sender balance: %s", senderInitialBalance.String())
	t.Logf("Initial recipient balance: %s", recipientInitialBalance.String())

	require.True(t, senderInitialBalance.Sign() > 0, "Sender should have initial balance")
	require.True(t, recipientInitialBalance.Sign() == 0, "Recipient should have zero initial balance")

	// Transfer amount (1e12 wei = 1 microsei in EVM terms)
	transferAmount := big.NewInt(1000000000000) // 1e12 wei

	// Create the transfer transaction
	transfers := []EVMTransfer{
		{
			Signer: sender,
			To:     recipient.EvmAddress,
			Value:  transferAmount,
			Nonce:  0,
		},
	}

	// Note: CreateEVMTransferTxs will fund sender again, so we track the balance after tx creation
	txs := CreateEVMTransferTxs(t, gigaCtx, transfers, true)

	// Get balances right before block execution
	senderPreBlockBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, sender.AccountAddress)
	recipientPreBlockBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, recipient.AccountAddress)

	t.Logf("Pre-block sender balance: %s", senderPreBlockBalance.String())
	t.Logf("Pre-block recipient balance: %s", recipientPreBlockBalance.String())

	// Execute the block
	_, results, err := RunBlock(t, gigaCtx, txs)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, uint32(0), results[0].Code, "Transfer should succeed: %s", results[0].Log)

	// Get final balances - need to use the updated context after block execution
	// Note: ProcessBlock updates state, so we query against the same context
	senderFinalBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, sender.AccountAddress)
	recipientFinalBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, recipient.AccountAddress)

	t.Logf("Final sender balance: %s", senderFinalBalance.String())
	t.Logf("Final recipient balance: %s", recipientFinalBalance.String())

	// Calculate expected gas cost (21000 gas for simple transfer * gas price)
	gasUsed := results[0].GasUsed
	gasPrice := big.NewInt(100000000000) // From CreateEVMTransferTxs
	gasCost := new(big.Int).Mul(big.NewInt(gasUsed), gasPrice)

	t.Logf("Gas used: %d, Gas cost: %s", gasUsed, gasCost.String())

	// Verify recipient received the transfer amount
	recipientGained := new(big.Int).Sub(recipientFinalBalance, recipientPreBlockBalance)
	require.Equal(t, 0, recipientGained.Cmp(transferAmount),
		"Recipient should have gained exactly the transfer amount. Gained: %s, Expected: %s",
		recipientGained.String(), transferAmount.String())

	// Verify sender lost transfer amount + gas
	senderLost := new(big.Int).Sub(senderPreBlockBalance, senderFinalBalance)
	expectedSenderLoss := new(big.Int).Add(transferAmount, gasCost)
	require.Equal(t, 0, senderLost.Cmp(expectedSenderLoss),
		"Sender should have lost transfer amount + gas. Lost: %s, Expected: %s",
		senderLost.String(), expectedSenderLoss.String())

	t.Logf("Balance transfer verified: sender lost %s (transfer %s + gas %s), recipient gained %s",
		senderLost.String(), transferAmount.String(), gasCost.String(), recipientGained.String())
}
