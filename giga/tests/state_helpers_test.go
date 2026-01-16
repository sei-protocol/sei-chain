package giga_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	gigalib "github.com/sei-protocol/sei-chain/giga/executor/lib"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
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

	// Create a minimal test account for app initialization
	testAcct := utils.NewSigner()

	wrapper := app.NewTestWrapper(t, blockTime, testAcct.PublicKey, true, func(ba *baseapp.BaseApp) {
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

// stTransaction is the JSON representation of a state test transaction
type stTransaction struct {
	GasPrice             string                 `json:"gasPrice"`
	MaxFeePerGas         string                 `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string                 `json:"maxPriorityFeePerGas"`
	Nonce                string                 `json:"nonce"`
	To                   string                 `json:"to"`
	Data                 []string               `json:"data"`
	AccessLists          []*ethtypes.AccessList `json:"accessLists,omitempty"`
	GasLimit             []string               `json:"gasLimit"`
	Value                []string               `json:"value"`
	PrivateKey           hexutil.Bytes          `json:"secretKey"`
	Sender               common.Address         `json:"sender"`
}

// stJSON is the JSON representation of a state test
type stJSON struct {
	Pre         ethtypes.GenesisAlloc `json:"pre"`
	Env         stEnv                 `json:"env"`
	Transaction stTransaction         `json:"transaction"`
	Post        map[string][]stPost   `json:"post"`
}

type stEnv struct {
	Coinbase   common.Address `json:"currentCoinbase"`
	Difficulty string         `json:"currentDifficulty"`
	GasLimit   string         `json:"currentGasLimit"`
	Number     string         `json:"currentNumber"`
	Timestamp  string         `json:"currentTimestamp"`
	BaseFee    string         `json:"currentBaseFee"`
	Random     *common.Hash   `json:"currentRandom"`
}

type stPost struct {
	Root            common.Hash           `json:"hash"`
	Logs            common.Hash           `json:"logs"`
	TxBytes         hexutil.Bytes         `json:"txbytes"`
	ExpectException string                `json:"expectException"`
	State           ethtypes.GenesisAlloc `json:"state"`
	Indexes         struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	} `json:"indexes"`
}

// LoadStateTest loads a state test from a JSON file
func LoadStateTest(t testing.TB, filePath string) map[string]*stJSON {
	file, err := os.Open(filepath.Clean(filePath))
	require.NoError(t, err, "failed to open test file")
	defer file.Close()

	var tests map[string]stJSON
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	require.NoError(t, err, "failed to decode test file")

	result := make(map[string]*stJSON)
	for name, test := range tests {
		testCopy := test
		result[name] = &testCopy
	}
	return result
}

// LoadStateTestsFromDir loads all state tests from a directory
func LoadStateTestsFromDir(t testing.TB, dirPath string) map[string]*stJSON {
	result := make(map[string]*stJSON)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		tests := LoadStateTest(t, path)
		for name, test := range tests {
			// Use relative path + test name as key to avoid collisions
			relPath, _ := filepath.Rel(dirPath, path)
			fullName := filepath.Join(relPath, name)
			result[fullName] = test
		}
		return nil
	})
	require.NoError(t, err, "failed to walk test directory")

	return result
}

// parseHexBig parses a hex string (with possible leading zeros) to *big.Int
func parseHexBig(s string) *big.Int {
	if s == "" {
		return new(big.Int)
	}
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	result, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return new(big.Int)
	}
	return result
}

// parseHexUint64 parses a hex string to uint64
func parseHexUint64(s string) uint64 {
	return parseHexBig(s).Uint64()
}

// BuildTransaction creates an Ethereum transaction from state test data
// Note: Rebuilds the transaction with Sei's chain ID since fixtures use chain ID 1
func BuildTransaction(t testing.TB, st *stJSON, subtest stPost) (*ethtypes.Transaction, common.Address) {
	// Get the private key
	privateKey, err := crypto.ToECDSA(st.Transaction.PrivateKey)
	require.NoError(t, err, "invalid private key")

	// Use sender from transaction if available, otherwise derive from key
	var sender common.Address
	if st.Transaction.Sender != (common.Address{}) {
		sender = st.Transaction.Sender
	} else {
		sender = crypto.PubkeyToAddress(privateKey.PublicKey)
	}

	// Get indexed values
	dataIdx := subtest.Indexes.Data
	gasIdx := subtest.Indexes.Gas
	valueIdx := subtest.Indexes.Value

	// Parse data
	var data []byte
	if dataIdx < len(st.Transaction.Data) {
		dataHex := st.Transaction.Data[dataIdx]
		if dataHex != "" {
			data = common.FromHex(dataHex)
		}
	}

	// Parse gas limit
	var gasLimit uint64 = 21000
	if gasIdx < len(st.Transaction.GasLimit) {
		gasLimit = parseHexUint64(st.Transaction.GasLimit[gasIdx])
	}

	// Parse value
	value := new(big.Int)
	if valueIdx < len(st.Transaction.Value) {
		value = parseHexBig(st.Transaction.Value[valueIdx])
	}

	// Parse to address
	var to *common.Address
	if st.Transaction.To != "" {
		addr := common.HexToAddress(st.Transaction.To)
		to = &addr
	}

	// Parse nonce
	nonce := parseHexUint64(st.Transaction.Nonce)

	// Parse gas prices
	gasPrice := parseHexBig(st.Transaction.GasPrice)
	maxFeePerGas := parseHexBig(st.Transaction.MaxFeePerGas)
	maxPriorityFeePerGas := parseHexBig(st.Transaction.MaxPriorityFeePerGas)

	// Determine transaction type and create accordingly
	var tx *ethtypes.Transaction

	if maxFeePerGas.Sign() > 0 || st.Transaction.MaxFeePerGas != "" {
		// EIP-1559 transaction
		var accessList ethtypes.AccessList
		if subtest.Indexes.Data < len(st.Transaction.AccessLists) && st.Transaction.AccessLists[subtest.Indexes.Data] != nil {
			accessList = *st.Transaction.AccessLists[subtest.Indexes.Data]
		}

		tx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:    big.NewInt(config.DefaultChainID), // Use Sei's chain ID
			Nonce:      nonce,
			GasTipCap:  maxPriorityFeePerGas,
			GasFeeCap:  maxFeePerGas,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		})
	} else if len(st.Transaction.AccessLists) > 0 {
		// EIP-2930 transaction
		var accessList ethtypes.AccessList
		if subtest.Indexes.Data < len(st.Transaction.AccessLists) && st.Transaction.AccessLists[subtest.Indexes.Data] != nil {
			accessList = *st.Transaction.AccessLists[subtest.Indexes.Data]
		}

		tx = ethtypes.NewTx(&ethtypes.AccessListTx{
			ChainID:    big.NewInt(config.DefaultChainID),
			Nonce:      nonce,
			GasPrice:   gasPrice,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		})
	} else {
		// Legacy transaction
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       to,
			Value:    value,
			Data:     data,
		})
	}

	// Sign the transaction with Sei's chain ID
	signer := ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID))
	signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
	require.NoError(t, err, "failed to sign transaction")

	return signedTx, sender
}

// EncodeTxForApp encodes a signed transaction for the Sei app
func EncodeTxForApp(t testing.TB, signedTx *ethtypes.Transaction) []byte {
	tc := app.MakeEncodingConfig().TxConfig

	txData, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err, "failed to create tx data")

	msg, err := types.NewMsgEVMTransaction(txData)
	require.NoError(t, err, "failed to create EVM message")

	txBuilder := tc.NewTxBuilder()
	err = txBuilder.SetMsgs(msg)
	require.NoError(t, err, "failed to set messages")
	txBuilder.SetGasLimit(10000000000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))))

	txBytes, err := tc.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err, "failed to encode transaction")

	return txBytes
}

// RunStateTestBlock executes a state test transaction and returns results
func RunStateTestBlock(t testing.TB, stc *StateTestContext, txs [][]byte) ([]abci.Event, []*abci.ExecTxResult, error) {
	app.EnableOCC = stc.Mode == ModeV2withOCC || stc.Mode == ModeGigaOCC

	req := &abci.RequestFinalizeBlock{
		Txs:    txs,
		Height: stc.Ctx.BlockHeader().Height,
	}

	events, results, _, err := stc.TestApp.ProcessBlock(stc.Ctx, txs, req, req.DecidedLastCommit, false)
	return events, results, err
}

// extractOnce ensures we only extract the archive once per test run
var extractOnce sync.Once

// getStateTestsPath returns the path to GeneralStateTests, extracting from archive if needed
func getStateTestsPath(t testing.TB) string {
	dataPath := filepath.Join("data", "GeneralStateTests")
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath
	}

	archivePath := filepath.Join("data", "fixtures_general_state_tests.tgz")
	if _, err := os.Stat(archivePath); err == nil {
		extractOnce.Do(func() {
			t.Log("Extracting test fixtures from archive...")
			extractArchive(t, archivePath, "data")
		})
		if _, err := os.Stat(dataPath); err == nil {
			return dataPath
		}
	}

	t.Skip("No test fixtures available")
	return ""
}

// extractArchive extracts a .tgz archive to the destination directory
func extractArchive(t testing.TB, archivePath, destDir string) {
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", destDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to extract test fixtures: %v\nOutput: %s", err, output)
	}
}

// VerifyPostState verifies that the actual state matches the expected post-state from the fixture.
// This follows the same pattern as x/evm/keeper/replay.go:VerifyAccount() but adapted for test assertions.
// Note: Balance verification is skipped due to Sei's gas refund modifications
// (see https://github.com/sei-protocol/go-ethereum/pull/32)
func VerifyPostState(t *testing.T, stc *StateTestContext, expectedState ethtypes.GenesisAlloc, executorName string) {
	for addr, expectedAccount := range expectedState {
		// Verify storage
		for key, expectedValue := range expectedAccount.Storage {
			actualValue := stc.TestApp.EvmKeeper.GetState(stc.Ctx, addr, key)
			if !bytes.Equal(expectedValue.Bytes(), actualValue.Bytes()) {
				t.Errorf("%s: storage mismatch for %s key %s: expected %s, got %s",
					executorName, addr.Hex(), key.Hex(), expectedValue.Hex(), actualValue.Hex())
			}
		}

		// Verify nonce
		actualNonce := stc.TestApp.EvmKeeper.GetNonce(stc.Ctx, addr)
		if expectedAccount.Nonce != actualNonce {
			t.Errorf("%s: nonce mismatch for %s: expected %d, got %d",
				executorName, addr.Hex(), expectedAccount.Nonce, actualNonce)
		}

		// Verify code
		actualCode := stc.TestApp.EvmKeeper.GetCode(stc.Ctx, addr)
		if !bytes.Equal(expectedAccount.Code, actualCode) {
			t.Errorf("%s: code mismatch for %s: expected %d bytes, got %d bytes",
				executorName, addr.Hex(), len(expectedAccount.Code), len(actualCode))
		}

		// Note: Balance verification is intentionally skipped due to Sei-specific gas handling
		// (limiting EVM max refund to 150% of used gas)
	}
}

// runStateTestComparison runs a state test through both V2 and Giga and compares results
func runStateTestComparison(t *testing.T, st *stJSON, post stPost) {
	blockTime := time.Now()

	// Build the transaction
	signedTx, sender := BuildTransaction(t, st, post)
	txBytes := EncodeTxForApp(t, signedTx)

	// --- Run with V2 Sequential (baseline) ---
	v2Ctx := NewStateTestContext(t, blockTime, 1, ModeV2Sequential)
	v2Ctx.SetupPreState(t, st.Pre)
	// Associate sender address
	senderSei := v2Ctx.TestApp.EvmKeeper.GetSeiAddressOrDefault(v2Ctx.Ctx, sender)
	v2Ctx.TestApp.EvmKeeper.SetAddressMapping(v2Ctx.Ctx, senderSei, sender)

	_, v2Results, v2Err := RunStateTestBlock(t, v2Ctx, [][]byte{txBytes})

	// --- Run with Giga ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, ModeGigaSequential)
	gigaCtx.SetupPreState(t, st.Pre)
	// Associate sender address
	senderSeiGiga := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, sender)
	gigaCtx.TestApp.EvmKeeper.SetAddressMapping(gigaCtx.Ctx, senderSeiGiga, sender)

	_, gigaResults, gigaErr := RunStateTestBlock(t, gigaCtx, [][]byte{txBytes})

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
		VerifyPostState(t, v2Ctx, post.State, "V2")
		VerifyPostState(t, gigaCtx, post.State, "Giga")
	}
}
