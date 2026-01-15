package giga_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	gigalib "github.com/sei-protocol/sei-chain/giga/executor/lib"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// ExecutorMode defines which executor path to use
type ExecutorMode int

const (
	ModeV2withOCC      ExecutorMode = iota // V2 execution path with OCC (standard production path)
	ModeGigaSequential                     // Giga executor, no OCC
	ModeGigaOCC                            // Giga executor with OCC
)

func (m ExecutorMode) String() string {
	switch m {
	case ModeV2withOCC:
		return "V2withOCC"
	case ModeGigaSequential:
		return "GigaSequential"
	case ModeGigaOCC:
		return "GigaOCC"
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

// NewGigaTestContext creates a test context configured for a specific executor mode
func NewGigaTestContext(t testing.TB, testAccts []utils.TestAcct, blockTime time.Time, workers int, mode ExecutorMode) *GigaTestContext {
	// OCC is enabled for both GethOCC and GigaOCC modes
	occEnabled := mode == ModeV2withOCC || mode == ModeGigaOCC
	gigaEnabled := mode == ModeGigaSequential || mode == ModeGigaOCC
	gigaOCCEnabled := mode == ModeGigaOCC

	wrapper := app.NewTestWrapper(t, blockTime, testAccts[0].PublicKey, true, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(occEnabled)
		ba.SetConcurrencyWorkers(workers)
	})
	testApp := wrapper.App
	ctx := wrapper.Ctx
	ctx = ctx.WithBlockHeader(tmproto.Header{
		Height:  ctx.BlockHeader().Height,
		ChainID: ctx.BlockHeader().ChainID,
		Time:    blockTime,
	})

	// Configure giga executor
	testApp.EvmKeeper.GigaExecutorEnabled = gigaEnabled
	testApp.EvmKeeper.GigaOCCEnabled = gigaOCCEnabled
	if gigaEnabled {
		evmoneVM, err := gigalib.InitEvmoneVM()
		if err != nil {
			t.Fatalf("failed to load evmone: %v", err)
		}
		testApp.EvmKeeper.EvmoneVM = evmoneVM
	}

	// Fund test accounts
	amounts := sdk.NewCoins(
		sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
		sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
	)
	for _, ta := range testAccts {
		err := testApp.BankKeeper.MintCoins(ctx, "mint", amounts)
		if err != nil {
			t.Fatalf("failed to mint coins: %v", err)
		}
		err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", ta.AccountAddress, amounts)
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
func CreateEVMTransferTxs(t testing.TB, tCtx *GigaTestContext, transfers []EVMTransfer) [][]byte {
	txs := make([][]byte, 0, len(transfers))
	tc := app.MakeEncodingConfig().TxConfig

	for _, transfer := range transfers {
		// Associate the Cosmos address with the EVM address
		// This is required for the Giga executor path which bypasses ante handlers
		tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, transfer.Signer.AccountAddress, transfer.Signer.EvmAddress)

		// Fund the signer account before creating the transaction
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := tCtx.TestApp.BankKeeper.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = tCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", transfer.Signer.AccountAddress, amounts)
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

// CompareResults compares execution results between two runs
func CompareResults(t *testing.T, testName string, expected, actual []*abci.ExecTxResult) {
	require.Equal(t, len(expected), len(actual), "%s: result count mismatch", testName)

	for i := range expected {
		if expected[i].Code != actual[i].Code {
			t.Logf("%s: tx[%d] expected code=%d log=%q", testName, i, expected[i].Code, expected[i].Log)
			t.Logf("%s: tx[%d] actual   code=%d log=%q", testName, i, actual[i].Code, actual[i].Log)
		}
		require.Equal(t, expected[i].Code, actual[i].Code,
			"%s: tx[%d] code mismatch (expected %d, got %d)", testName, i, expected[i].Code, actual[i].Code)

		// For successful txs, compare gas used (allow small variance for different execution paths)
		if expected[i].Code == 0 && actual[i].Code == 0 {
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
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers)
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
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers)
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
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")
	require.Len(t, seqResults, txCount)

	// Run with Giga OCC (multiple times to catch race conditions)
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers)
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
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")
	require.Len(t, seqResults, txCount)

	// Run with Giga OCC (multiple times to catch race conditions)
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers)
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
	seqTxs := CreateEVMTransferTxs(t, seqCtx, transfers)
	_, seqResults, seqErr := RunBlock(t, seqCtx, seqTxs)
	require.NoError(t, seqErr, "Giga sequential execution failed")

	// Run with Giga OCC
	for run := 0; run < 3; run++ {
		occCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
		occTxs := CreateEVMTransferTxs(t, occCtx, transfers)
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
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr)

	// Giga Sequential
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqTxs := CreateEVMTransferTxs(t, gigaSeqCtx, transfers)
	_, gigaSeqResults, gigaSeqErr := RunBlock(t, gigaSeqCtx, gigaSeqTxs)
	require.NoError(t, gigaSeqErr)

	// Giga OCC
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCTxs := CreateEVMTransferTxs(t, gigaOCCCtx, transfers)
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
	gethTxs := CreateEVMTransferTxs(t, gethCtx, transfers)
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr)

	// Giga Sequential
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqTxs := CreateEVMTransferTxs(t, gigaSeqCtx, transfers)
	_, gigaSeqResults, gigaSeqErr := RunBlock(t, gigaSeqCtx, gigaSeqTxs)
	require.NoError(t, gigaSeqErr)

	// Giga OCC
	gigaOCCCtx := NewGigaTestContext(t, accts, blockTime, workers, ModeGigaOCC)
	gigaOCCTxs := CreateEVMTransferTxs(t, gigaOCCCtx, transfers)
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
