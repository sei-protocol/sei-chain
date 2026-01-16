package giga_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtype "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	gigalib "github.com/sei-protocol/sei-chain/giga/executor/lib"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
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

// CosmosBankSend represents a Cosmos bank send transaction for testing
type CosmosBankSend struct {
	Signer utils.TestAcct
	To     sdk.AccAddress
	Amount sdk.Coins
}

// OracleVote represents an oracle vote transaction for testing (prioritized tx)
type OracleVote struct {
	Signer        utils.TestAcct
	ExchangeRates string // e.g., "1.5usei,2.0uatom"
}

// CreateOracleVoteTxs creates signed oracle vote transactions (these are prioritized)
func CreateOracleVoteTxs(t testing.TB, tCtx *GigaTestContext, votes []OracleVote) [][]byte {
	txs := make([][]byte, 0, len(votes))
	tc := app.MakeEncodingConfig().TxConfig

	for _, vote := range votes {
		// Get account for sequence number
		acct := tCtx.TestApp.AccountKeeper.GetAccount(tCtx.Ctx, vote.Signer.AccountAddress)
		if acct == nil {
			// Create account if it doesn't exist
			acct = tCtx.TestApp.AccountKeeper.NewAccountWithAddress(tCtx.Ctx, vote.Signer.AccountAddress)
			tCtx.TestApp.AccountKeeper.SetAccount(tCtx.Ctx, acct)
		}

		// Fund the sender
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := tCtx.TestApp.BankKeeper.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = tCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", vote.Signer.AccountAddress, amounts)
		require.NoError(t, err)

		// Create the MsgAggregateExchangeRateVote (this is a prioritized oracle tx)
		msg := oracletypes.NewMsgAggregateExchangeRateVote(
			vote.ExchangeRates,
			vote.Signer.AccountAddress,
			vote.Signer.ValidatorAddress,
		)

		// Wrap in Any
		a, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)

		// Build the transaction
		tBuilder := tx.WrapTx(&txtype.Tx{
			Body: &txtype.TxBody{
				Messages: []*codectypes.Any{a},
			},
			AuthInfo: &txtype.AuthInfo{
				Fee: &txtype.Fee{
					Amount:   sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))),
					GasLimit: 10000000000,
					Payer:    vote.Signer.AccountAddress.String(),
					Granter:  vote.Signer.AccountAddress.String(),
				},
			},
		})

		// Set empty signature first
		err = tBuilder.SetSignatures(signing.SignatureV2{
			PubKey: vote.Signer.PrivateKey.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  tc.SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: acct.GetSequence(),
		})
		require.NoError(t, err)

		// Create signer data
		signerData := authsigning.SignerData{
			ChainID:       tCtx.Ctx.ChainID(),
			Sequence:      acct.GetSequence(),
			AccountNumber: acct.GetAccountNumber(),
		}

		// Sign the transaction
		sigV2, err := clienttx.SignWithPrivKey(
			tc.SignModeHandler().DefaultMode(), signerData,
			tBuilder, vote.Signer.PrivateKey, tc, acct.GetSequence())
		require.NoError(t, err)

		err = tBuilder.SetSignatures(sigV2)
		require.NoError(t, err)

		// Encode to bytes
		txBytes, err := tc.TxEncoder()(tBuilder.GetTx())
		require.NoError(t, err)

		txs = append(txs, txBytes)

		// Increment sequence for next tx from this sender
		err = acct.SetSequence(acct.GetSequence() + 1)
		require.NoError(t, err)
	}

	return txs
}

// CreateCosmosBankSendTxs creates signed Cosmos bank send transactions
func CreateCosmosBankSendTxs(t testing.TB, tCtx *GigaTestContext, sends []CosmosBankSend) [][]byte {
	txs := make([][]byte, 0, len(sends))
	tc := app.MakeEncodingConfig().TxConfig

	for _, send := range sends {
		// Get account for sequence number
		acct := tCtx.TestApp.AccountKeeper.GetAccount(tCtx.Ctx, send.Signer.AccountAddress)
		if acct == nil {
			// Create account if it doesn't exist
			acct = tCtx.TestApp.AccountKeeper.NewAccountWithAddress(tCtx.Ctx, send.Signer.AccountAddress)
			tCtx.TestApp.AccountKeeper.SetAccount(tCtx.Ctx, acct)
		}

		// Fund the sender
		amounts := sdk.NewCoins(
			sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)),
			sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)),
		)
		err := tCtx.TestApp.BankKeeper.MintCoins(tCtx.Ctx, "mint", amounts)
		require.NoError(t, err)
		err = tCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(tCtx.Ctx, "mint", send.Signer.AccountAddress, amounts)
		require.NoError(t, err)

		// Create the MsgSend
		msg := banktypes.NewMsgSend(send.Signer.AccountAddress, send.To, send.Amount)

		// Wrap in Any
		a, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)

		// Build the transaction
		tBuilder := tx.WrapTx(&txtype.Tx{
			Body: &txtype.TxBody{
				Messages: []*codectypes.Any{a},
			},
			AuthInfo: &txtype.AuthInfo{
				Fee: &txtype.Fee{
					Amount:   sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))),
					GasLimit: 10000000000,
					Payer:    send.Signer.AccountAddress.String(),
					Granter:  send.Signer.AccountAddress.String(),
				},
			},
		})

		// Set empty signature first (needed for sign bytes generation)
		err = tBuilder.SetSignatures(signing.SignatureV2{
			PubKey: send.Signer.PrivateKey.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  tc.SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: acct.GetSequence(),
		})
		require.NoError(t, err)

		// Create signer data
		signerData := authsigning.SignerData{
			ChainID:       tCtx.Ctx.ChainID(),
			Sequence:      acct.GetSequence(),
			AccountNumber: acct.GetAccountNumber(),
		}

		// Sign the transaction
		sigV2, err := clienttx.SignWithPrivKey(
			tc.SignModeHandler().DefaultMode(), signerData,
			tBuilder, send.Signer.PrivateKey, tc, acct.GetSequence())
		require.NoError(t, err)

		err = tBuilder.SetSignatures(sigV2)
		require.NoError(t, err)

		// Encode to bytes
		txBytes, err := tc.TxEncoder()(tBuilder.GetTx())
		require.NoError(t, err)

		txs = append(txs, txBytes)

		// Increment sequence for next tx from this sender
		err = acct.SetSequence(acct.GetSequence() + 1)
		require.NoError(t, err)
	}

	return txs
}

// TestGigaVsGeth_SingleBalanceTransfer is a minimal test comparing Giga vs Geth
// for a single EVM balance transfer transaction.
// This test is useful for verifying that Giga produces identical results to Geth.
func TestGigaVsGeth_SingleBalanceTransfer(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(2)

	// Create a sender and recipient
	sender := utils.NewSigner()
	recipient := common.HexToAddress("0x1234567890123456789012345678901234567890")
	transferAmount := big.NewInt(1000000000000) // 1e12 wei

	// Single transfer
	transfer := EVMTransfer{
		Signer: sender,
		To:     recipient,
		Value:  transferAmount,
		Nonce:  0,
	}

	// ---- Run with Geth (old way / baseline) ----
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)
	gethTxs := CreateEVMTransferTxs(t, gethCtx, []EVMTransfer{transfer})
	_, gethResults, gethErr := RunBlock(t, gethCtx, gethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, 1, "Expected exactly 1 result from Geth")

	// ---- Run with Giga (new way) ----
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaTxs := CreateEVMTransferTxs(t, gigaCtx, []EVMTransfer{transfer})
	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, gigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, 1, "Expected exactly 1 result from Giga")

	// ---- Verify both transactions succeeded ----
	gethResult := gethResults[0]
	gigaResult := gigaResults[0]

	require.Equal(t, uint32(0), gethResult.Code, "Geth transfer failed: %s", gethResult.Log)
	require.Equal(t, uint32(0), gigaResult.Code, "Giga transfer failed: %s", gigaResult.Log)

	// ---- Detailed comparison ----
	t.Logf("=== Single Balance Transfer Comparison ===")
	t.Logf("Geth result - Code: %d, GasUsed: %d", gethResult.Code, gethResult.GasUsed)
	t.Logf("Giga result - Code: %d, GasUsed: %d", gigaResult.Code, gigaResult.GasUsed)

	// Compare execution results
	require.Equal(t, gethResult.Code, gigaResult.Code, "Result code mismatch")
	require.Equal(t, gethResult.GasUsed, gigaResult.GasUsed, "Gas used mismatch")

	// Compare EvmTxInfo if present
	if gethResult.EvmTxInfo != nil {
		require.NotNil(t, gigaResult.EvmTxInfo, "Giga missing EvmTxInfo")
		require.Equal(t, gethResult.EvmTxInfo.TxHash, gigaResult.EvmTxInfo.TxHash, "TxHash mismatch")
		require.Equal(t, gethResult.EvmTxInfo.Nonce, gigaResult.EvmTxInfo.Nonce, "Nonce mismatch")
		require.Equal(t, gethResult.EvmTxInfo.VmError, gigaResult.EvmTxInfo.VmError, "VmError mismatch")

		t.Logf("Geth EvmTxInfo - TxHash: %s, Nonce: %d, VmError: %q",
			gethResult.EvmTxInfo.TxHash, gethResult.EvmTxInfo.Nonce, gethResult.EvmTxInfo.VmError)
		t.Logf("Giga EvmTxInfo - TxHash: %s, Nonce: %d, VmError: %q",
			gigaResult.EvmTxInfo.TxHash, gigaResult.EvmTxInfo.Nonce, gigaResult.EvmTxInfo.VmError)
	}

	// Verify recipient balance after transfer (check state)
	// Convert EVM address to Sei address for balance lookup
	gethRecipientSeiAddr := gethCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gethCtx.Ctx, recipient)
	gigaRecipientSeiAddr := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, recipient)
	gethRecipientBalance := gethCtx.TestApp.EvmKeeper.GetBalance(gethCtx.Ctx, gethRecipientSeiAddr)
	gigaRecipientBalance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, gigaRecipientSeiAddr)

	t.Logf("Geth recipient balance: %s", gethRecipientBalance.String())
	t.Logf("Giga recipient balance: %s", gigaRecipientBalance.String())

	require.Equal(t, gethRecipientBalance.String(), gigaRecipientBalance.String(),
		"Recipient balance mismatch after transfer")

	t.Logf("=== SUCCESS: Giga and Geth produced identical results ===")
}

// TestGigaVsGeth_CosmosThenEVM is a test comparing Giga vs Geth when executing
// an interleaved sequence of Cosmos and EVM transactions in the same block:
// Cosmos -> EVM -> Cosmos -> EVM
// This verifies that the mix of Cosmos and EVM transactions produces identical results.
func TestGigaVsGeth_CosmosThenEVM(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(5)

	// ---- Setup transaction parameters ----

	// Cosmos tx 1: accts[0] -> accts[1]
	cosmosSender1 := accts[0]
	cosmosRecipient1 := accts[1].AccountAddress
	cosmosAmount1 := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))) // 1 SEI

	// EVM tx 1: new signer -> EVM address 1
	evmSender1 := utils.NewSigner()
	evmRecipient1 := common.HexToAddress("0xDEADBEEF00000000000000000000000000000001")
	evmAmount1 := big.NewInt(1000000000000) // 1e12 wei

	// Cosmos tx 2: accts[2] -> accts[3]
	cosmosSender2 := accts[2]
	cosmosRecipient2 := accts[3].AccountAddress
	cosmosAmount2 := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(2000000))) // 2 SEI

	// EVM tx 2: new signer -> EVM address 2
	evmSender2 := utils.NewSigner()
	evmRecipient2 := common.HexToAddress("0xDEADBEEF00000000000000000000000000000002")
	evmAmount2 := big.NewInt(2000000000000) // 2e12 wei

	// ---- Run with Geth (old way / baseline) ----
	t.Log("=== Running with Geth (baseline) ===")
	gethCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeV2withOCC)

	// Create transactions in order: Cosmos1, EVM1, Cosmos2, EVM2
	gethCosmosTx1 := CreateCosmosBankSendTxs(t, gethCtx, []CosmosBankSend{
		{Signer: cosmosSender1, To: cosmosRecipient1, Amount: cosmosAmount1},
	})
	gethEvmTx1 := CreateEVMTransferTxs(t, gethCtx, []EVMTransfer{
		{Signer: evmSender1, To: evmRecipient1, Value: evmAmount1, Nonce: 0},
	})
	gethCosmosTx2 := CreateCosmosBankSendTxs(t, gethCtx, []CosmosBankSend{
		{Signer: cosmosSender2, To: cosmosRecipient2, Amount: cosmosAmount2},
	})
	gethEvmTx2 := CreateEVMTransferTxs(t, gethCtx, []EVMTransfer{
		{Signer: evmSender2, To: evmRecipient2, Value: evmAmount2, Nonce: 0},
	})

	// Combine in order: Cosmos -> EVM -> Cosmos -> EVM
	allGethTxs := append(gethCosmosTx1, gethEvmTx1...)
	allGethTxs = append(allGethTxs, gethCosmosTx2...)
	allGethTxs = append(allGethTxs, gethEvmTx2...)

	_, gethResults, gethErr := RunBlock(t, gethCtx, allGethTxs)
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, 4, "Expected exactly 4 results from Geth")

	// ---- Run with Giga (new way) ----
	t.Log("=== Running with Giga ===")
	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)

	// Create transactions in the same order
	gigaCosmosTx1 := CreateCosmosBankSendTxs(t, gigaCtx, []CosmosBankSend{
		{Signer: cosmosSender1, To: cosmosRecipient1, Amount: cosmosAmount1},
	})
	gigaEvmTx1 := CreateEVMTransferTxs(t, gigaCtx, []EVMTransfer{
		{Signer: evmSender1, To: evmRecipient1, Value: evmAmount1, Nonce: 0},
	})
	gigaCosmosTx2 := CreateCosmosBankSendTxs(t, gigaCtx, []CosmosBankSend{
		{Signer: cosmosSender2, To: cosmosRecipient2, Amount: cosmosAmount2},
	})
	gigaEvmTx2 := CreateEVMTransferTxs(t, gigaCtx, []EVMTransfer{
		{Signer: evmSender2, To: evmRecipient2, Value: evmAmount2, Nonce: 0},
	})

	// Combine in order: Cosmos -> EVM -> Cosmos -> EVM
	allGigaTxs := append(gigaCosmosTx1, gigaEvmTx1...)
	allGigaTxs = append(allGigaTxs, gigaCosmosTx2...)
	allGigaTxs = append(allGigaTxs, gigaEvmTx2...)

	_, gigaResults, gigaErr := RunBlock(t, gigaCtx, allGigaTxs)
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, 4, "Expected exactly 4 results from Giga")

	// ---- Compare Results ----
	t.Log("=== Comparing Results ===")

	txLabels := []string{"Cosmos1", "EVM1", "Cosmos2", "EVM2"}
	evmIndices := map[int]bool{1: true, 3: true} // indices of EVM transactions

	for i := 0; i < 4; i++ {
		gethResult := gethResults[i]
		gigaResult := gigaResults[i]

		t.Logf("%s - Geth: Code=%d, GasUsed=%d, Log=%q",
			txLabels[i], gethResult.Code, gethResult.GasUsed, gethResult.Log)
		t.Logf("%s - Giga: Code=%d, GasUsed=%d, Log=%q",
			txLabels[i], gigaResult.Code, gigaResult.GasUsed, gigaResult.Log)

		require.Equal(t, gethResult.Code, gigaResult.Code,
			"%s: result code mismatch", txLabels[i])
		require.Equal(t, gethResult.GasUsed, gigaResult.GasUsed,
			"%s: gas used mismatch", txLabels[i])

		// Compare EvmTxInfo for EVM transactions
		if evmIndices[i] {
			if gethResult.EvmTxInfo != nil {
				require.NotNil(t, gigaResult.EvmTxInfo, "Giga missing EvmTxInfo for %s", txLabels[i])
				require.Equal(t, gethResult.EvmTxInfo.TxHash, gigaResult.EvmTxInfo.TxHash,
					"%s: TxHash mismatch", txLabels[i])
				require.Equal(t, gethResult.EvmTxInfo.Nonce, gigaResult.EvmTxInfo.Nonce,
					"%s: Nonce mismatch", txLabels[i])
				require.Equal(t, gethResult.EvmTxInfo.VmError, gigaResult.EvmTxInfo.VmError,
					"%s: VmError mismatch", txLabels[i])
			}
		}
	}

	// ---- Verify state changes ----
	t.Log("=== Verifying Final State ===")

	// Check Cosmos recipient 1 balance
	gethCosmosRecipient1Balance := gethCtx.TestApp.BankKeeper.GetBalance(gethCtx.Ctx, cosmosRecipient1, "usei")
	gigaCosmosRecipient1Balance := gigaCtx.TestApp.BankKeeper.GetBalance(gigaCtx.Ctx, cosmosRecipient1, "usei")
	t.Logf("Cosmos recipient 1 balance - Geth: %s, Giga: %s",
		gethCosmosRecipient1Balance.String(), gigaCosmosRecipient1Balance.String())
	require.Equal(t, gethCosmosRecipient1Balance.String(), gigaCosmosRecipient1Balance.String(),
		"Cosmos recipient 1 balance mismatch")

	// Check Cosmos recipient 2 balance
	gethCosmosRecipient2Balance := gethCtx.TestApp.BankKeeper.GetBalance(gethCtx.Ctx, cosmosRecipient2, "usei")
	gigaCosmosRecipient2Balance := gigaCtx.TestApp.BankKeeper.GetBalance(gigaCtx.Ctx, cosmosRecipient2, "usei")
	t.Logf("Cosmos recipient 2 balance - Geth: %s, Giga: %s",
		gethCosmosRecipient2Balance.String(), gigaCosmosRecipient2Balance.String())
	require.Equal(t, gethCosmosRecipient2Balance.String(), gigaCosmosRecipient2Balance.String(),
		"Cosmos recipient 2 balance mismatch")

	// Check EVM recipient 1 balance
	gethEvmRecipient1SeiAddr := gethCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gethCtx.Ctx, evmRecipient1)
	gigaEvmRecipient1SeiAddr := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, evmRecipient1)
	gethEvmRecipient1Balance := gethCtx.TestApp.EvmKeeper.GetBalance(gethCtx.Ctx, gethEvmRecipient1SeiAddr)
	gigaEvmRecipient1Balance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, gigaEvmRecipient1SeiAddr)
	t.Logf("EVM recipient 1 balance - Geth: %s, Giga: %s",
		gethEvmRecipient1Balance.String(), gigaEvmRecipient1Balance.String())
	require.Equal(t, gethEvmRecipient1Balance.String(), gigaEvmRecipient1Balance.String(),
		"EVM recipient 1 balance mismatch")

	// Check EVM recipient 2 balance
	gethEvmRecipient2SeiAddr := gethCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gethCtx.Ctx, evmRecipient2)
	gigaEvmRecipient2SeiAddr := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, evmRecipient2)
	gethEvmRecipient2Balance := gethCtx.TestApp.EvmKeeper.GetBalance(gethCtx.Ctx, gethEvmRecipient2SeiAddr)
	gigaEvmRecipient2Balance := gigaCtx.TestApp.EvmKeeper.GetBalance(gigaCtx.Ctx, gigaEvmRecipient2SeiAddr)
	t.Logf("EVM recipient 2 balance - Geth: %s, Giga: %s",
		gethEvmRecipient2Balance.String(), gigaEvmRecipient2Balance.String())
	require.Equal(t, gethEvmRecipient2Balance.String(), gigaEvmRecipient2Balance.String(),
		"EVM recipient 2 balance mismatch")

	t.Log("=== SUCCESS: Giga and Geth produced identical results for Cosmos -> EVM -> Cosmos -> EVM ===")
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
	})
	_, gethDeployResults, gethDeployErr := RunBlock(t, gethCtx, gethDeployTxs)
	require.NoError(t, gethDeployErr, "Geth deploy failed")
	require.Len(t, gethDeployResults, 1)
	require.Equal(t, uint32(0), gethDeployResults[0].Code, "Geth deploy tx failed: %s", gethDeployResults[0].Log)

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
	})

	gethCallInputs := make([]EVMContractCall, callCount)
	for i := 0; i < callCount; i++ {
		gethCallInputs[i] = EVMContractCall{
			Signer:   callers[i],
			Contract: contractAddr,
			Data:     encodeSetCall(big.NewInt(int64(i + 100))), // set(100), set(101), etc.
			Nonce:    0,
		}
	}
	gethCallTxs := CreateContractCallTxs(t, gethCtx, gethCallInputs)

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
	})
	gethCallTxs := CreateContractCallTxs(t, gethCtx, []EVMContractCall{
		{Signer: caller, Contract: contractAddr, Data: encodeSetCall(big.NewInt(42)), Nonce: 0},
	})
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
	// Skip gas for Geth vs Giga (different implementations), but compare gas for Giga variants
	CompareResultsNoGas(t, "AllModes_GethVsGigaSeq", gethResults, gigaSeqResults)
	CompareResults(t, "AllModes_GigaSeqVsOCC", gigaSeqResults, gigaOCCResults)

	t.Logf("Contract deployment and calls produced identical results across all three executor modes")
}
