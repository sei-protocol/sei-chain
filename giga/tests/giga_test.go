package giga_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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
	ModeV2Sequential                       // V2 execution path without OCC (sequential)
	ModeGigaSequential                     // Giga executor, no OCC
	ModeGigaOCC                            // Giga executor with OCC
)

func (m ExecutorMode) String() string {
	switch m {
	case ModeV2withOCC:
		return "V2withOCC"
	case ModeV2Sequential:
		return "V2Sequential"
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
	// OCC is enabled for both V2OCC and GigaOCC modes
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
	// Set global OCC flag based on mode (both V2OCC and GigaOCC use OCC)
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

// TestGigaVsV2_NonConflicting compares Giga executor vs V2 for non-conflicting EVM transfers
func TestGigaVsV2_NonConflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 10

	// Generate the same transfers for both runs
	transfers := GenerateNonConflictingTransfers(txCount)

	// Run with V2 (baseline)
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2Txs := CreateEVMTransferTxs(t, v2Ctx, transfers)
	_, v2Results, v2Err := RunBlock(t, v2Ctx, v2Txs)
	require.NoError(t, v2Err, "V2 execution failed")
	require.Len(t, v2Results, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, txCount)

	// Compare results
	CompareResults(t, "GigaVsV2_NonConflicting", v2Results, gigaResults)

	// Verify all transactions succeeded
	for i, result := range v2Results {
		require.Equal(t, uint32(0), result.Code, "V2 tx[%d] failed: %s", i, result.Log)
	}
	for i, result := range gigaResults {
		require.Equal(t, uint32(0), result.Code, "Giga tx[%d] failed: %s", i, result.Log)
	}
}

// TestGigaVsV2_Conflicting compares Giga executor vs V2 for conflicting EVM transfers
func TestGigaVsV2_Conflicting(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)
	txCount := 10

	// All transfers go to the same recipient (conflicting)
	recipient := accts[0].EvmAddress
	transfers := GenerateConflictingTransfers(txCount, recipient)

	// Run with V2 (baseline)
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2Txs := CreateEVMTransferTxs(t, v2Ctx, transfers)
	_, v2Results, v2Err := RunBlock(t, v2Ctx, v2Txs)
	require.NoError(t, v2Err, "V2 execution failed")
	require.Len(t, v2Results, txCount)

	// Run with Giga Sequential
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, transfers)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, txCount)

	// Compare results
	CompareResults(t, "GigaVsV2_Conflicting", v2Results, gigaResults)
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

	// V2 Sequential
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2Txs := CreateEVMTransferTxs(t, v2Ctx, transfers)
	_, v2Results, v2Err := RunBlock(t, v2Ctx, v2Txs)
	require.NoError(t, v2Err)

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

	// Compare: V2 vs Giga Sequential
	CompareResults(t, "AllModes_V2VsGigaSeq", v2Results, gigaSeqResults)

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

	// V2 Sequential
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2Txs := CreateEVMTransferTxs(t, v2Ctx, transfers)
	_, v2Results, v2Err := RunBlock(t, v2Ctx, v2Txs)
	require.NoError(t, v2Err)

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

	// Compare: V2 vs Giga Sequential
	CompareResults(t, "AllModes_Conflicting_V2VsGigaSeq", v2Results, gigaSeqResults)

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
func CreateContractDeployTxs(t testing.TB, tCtx *GigaTestContext, deploys []EVMContractDeploy) [][]byte {
	txs := make([][]byte, 0, len(deploys))
	tc := app.MakeEncodingConfig().TxConfig

	for _, deploy := range deploys {
		// Associate the Cosmos address with the EVM address
		tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, deploy.Signer.AccountAddress, deploy.Signer.EvmAddress)

		// Fund the signer account
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := tCtx.TestApp.BankKeeper.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = tCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", deploy.Signer.AccountAddress, amounts)
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
func CreateContractCallTxs(t testing.TB, tCtx *GigaTestContext, calls []EVMContractCall) [][]byte {
	txs := make([][]byte, 0, len(calls))
	tc := app.MakeEncodingConfig().TxConfig

	for _, call := range calls {
		// Associate the Cosmos address with the EVM address
		tCtx.TestApp.EvmKeeper.SetAddressMapping(tCtx.Ctx, call.Signer.AccountAddress, call.Signer.EvmAddress)

		// Fund the signer account
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := tCtx.TestApp.BankKeeper.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = tCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", call.Signer.AccountAddress, amounts)
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

// TestGigaVsV2_ContractDeployAndCall compares Giga vs V2 for contract deployment and calls
func TestGigaVsV2_ContractDeployAndCall(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// Create a deployer
	deployer := utils.NewSigner()

	// Deploy contract with V2
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2DeployTxs := CreateContractDeployTxs(t, v2Ctx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})
	_, v2DeployResults, v2DeployErr := RunBlock(t, v2Ctx, v2DeployTxs)
	require.NoError(t, v2DeployErr, "V2 deploy failed")
	require.Len(t, v2DeployResults, 1)
	require.Equal(t, uint32(0), v2DeployResults[0].Code, "V2 deploy tx failed: %s", v2DeployResults[0].Log)

	// Deploy contract with Giga
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})
	_, gigaDeployResults, gigaDeployErr := RunBlock(t, gigaCtx, gigaDeployTxs)
	require.NoError(t, gigaDeployErr, "Giga deploy failed")
	require.Len(t, gigaDeployResults, 1)
	require.Equal(t, uint32(0), gigaDeployResults[0].Code, "Giga deploy tx failed: %s", gigaDeployResults[0].Log)

	// Compare deployment results (skip gas comparison - different EVM implementations may differ)
	CompareResultsNoGas(t, "ContractDeploy", v2DeployResults, gigaDeployResults)

	t.Logf("Contract deployment successful on both V2 and Giga")
	t.Logf("V2 deploy gas used: %d", v2DeployResults[0].GasUsed)
	t.Logf("Giga deploy gas used: %d", gigaDeployResults[0].GasUsed)
}

// TestGigaVsV2_ContractCallsSequential compares Giga vs V2 for sequential contract calls
// This test deploys a contract and calls it within the same block
func TestGigaVsV2_ContractCallsSequential(t *testing.T) {
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

	// ---- Run with V2 ----
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)

	// Build all transactions: deploy + calls in same block
	v2DeployTxs := CreateContractDeployTxs(t, v2Ctx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})

	v2CallInputs := make([]EVMContractCall, callCount)
	for i := 0; i < callCount; i++ {
		v2CallInputs[i] = EVMContractCall{
			Signer:   callers[i],
			Contract: contractAddr,
			Data:     encodeSetCall(big.NewInt(int64(i + 100))), // set(100), set(101), etc.
			Nonce:    0,
		}
	}
	v2CallTxs := CreateContractCallTxs(t, v2Ctx, v2CallInputs)

	// Combine deploy + calls into one block
	allV2Txs := append(v2DeployTxs, v2CallTxs...)
	_, v2Results, v2Err := RunBlock(t, v2Ctx, allV2Txs)
	require.NoError(t, v2Err)
	require.Len(t, v2Results, 1+callCount)

	t.Logf("Contract deployed at: %s", contractAddr.Hex())

	// ---- Run with Giga ----
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	gigaDeployTxs := CreateContractDeployTxs(t, gigaCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})

	gigaCallInputs := make([]EVMContractCall, callCount)
	for i := 0; i < callCount; i++ {
		gigaCallInputs[i] = EVMContractCall{
			Signer:   callers[i],
			Contract: contractAddr,
			Data:     encodeSetCall(big.NewInt(int64(i + 100))),
			Nonce:    0,
		}
	}
	gigaCallTxs := CreateContractCallTxs(t, gigaCtx, gigaCallInputs)

	allGigaTxs := append(gigaDeployTxs, gigaCallTxs...)
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, allGigaTxs)
	require.NoError(t, gigaErr)
	require.Len(t, gigaResults, 1+callCount)

	// Compare results (skip gas comparison - different EVM implementations may differ)
	CompareResultsNoGas(t, "ContractDeployAndCalls", v2Results, gigaResults)

	// Verify all transactions succeeded
	for i, result := range v2Results {
		require.Equal(t, uint32(0), result.Code, "V2 tx[%d] failed: %s", i, result.Log)
	}
	for i, result := range gigaResults {
		require.Equal(t, uint32(0), result.Code, "Giga tx[%d] failed: %s", i, result.Log)
	}

	t.Logf("Contract deployment + %d calls successful on both V2 and Giga", callCount)
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

	// ---- V2 with OCC ----
	v2Ctx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	v2DeployTxs := CreateContractDeployTxs(t, v2Ctx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})
	v2CallTxs := CreateContractCallTxs(t, v2Ctx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	})
	allV2Txs := append(v2DeployTxs, v2CallTxs...)
	_, v2Results, err := RunBlock(t, v2Ctx, allV2Txs)
	require.NoError(t, err)
	require.Len(t, v2Results, 2)
	require.Equal(t, uint32(0), v2Results[0].Code, "V2 deploy failed: %s", v2Results[0].Log)
	require.Equal(t, uint32(0), v2Results[1].Code, "V2 call failed: %s", v2Results[1].Log)

	// ---- Giga Sequential ----
	gigaSeqCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaSeqDeployTxs := CreateContractDeployTxs(t, gigaSeqCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: simpleStorageBytecode, Nonce: 0},
	})
	gigaSeqCallTxs := CreateContractCallTxs(t, gigaSeqCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	})
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
	})
	gigaOCCCallTxs := CreateContractCallTxs(t, gigaOCCCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	})
	allGigaOCCTxs := append(gigaOCCDeployTxs, gigaOCCCallTxs...)
	_, gigaOCCResults, err := RunBlock(t, gigaOCCCtx, allGigaOCCTxs)
	require.NoError(t, err)
	require.Len(t, gigaOCCResults, 2)
	require.Equal(t, uint32(0), gigaOCCResults[0].Code, "GigaOCC deploy failed: %s", gigaOCCResults[0].Log)
	require.Equal(t, uint32(0), gigaOCCResults[1].Code, "GigaOCC call failed: %s", gigaOCCResults[1].Log)

	// Compare results
	// Skip gas for V2 vs Giga (different implementations), but compare gas for Giga variants
	CompareResultsNoGas(t, "AllModes_V2VsGigaSeq", v2Results, gigaSeqResults)
	CompareResults(t, "AllModes_GigaSeqVsOCC", gigaSeqResults, gigaOCCResults)

	t.Logf("Contract deployment and calls produced identical results across all three executor modes")
}
