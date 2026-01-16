package giga_test

import (
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
	testdataPath := filepath.Join("testdata", "GeneralStateTests")
	if _, err := os.Stat(testdataPath); err == nil {
		return testdataPath
	}

	archivePath := filepath.Join("testdata", "fixtures_general_state_tests.tgz")
	if _, err := os.Stat(archivePath); err == nil {
		extractOnce.Do(func() {
			t.Log("Extracting test fixtures from archive...")
			extractArchive(t, archivePath, "testdata")
		})
		if _, err := os.Stat(testdataPath); err == nil {
			return testdataPath
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

// TestGigaVsGeth_StateTests runs state tests comparing Giga vs Geth execution
func TestGigaVsGeth_StateTests(t *testing.T) {
	stateTestsPath := getStateTestsPath(t)

	// Allow filtering to specific directory via STATE_TEST_DIR env var
	specificDir := os.Getenv("STATE_TEST_DIR")

	var testDirs []string
	if specificDir != "" {
		// Run only the specified directory
		testDirs = []string{specificDir}
	} else {
		// Run all directories
		entries, err := os.ReadDir(stateTestsPath)
		require.NoError(t, err, "failed to read state tests directory")
		for _, entry := range entries {
			if entry.IsDir() {
				testDirs = append(testDirs, entry.Name())
			}
		}
	}

	for _, dir := range testDirs {
		dirPath := filepath.Join(stateTestsPath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Logf("Skipping %s - directory not found", dir)
			continue
		}

		tests := LoadStateTestsFromDir(t, dirPath)
		for testName, st := range tests {
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
					subtestName = testName + "/" + string(rune('0'+i))
				}

				t.Run(subtestName, func(t *testing.T) {
					runStateTestComparison(t, st, post)
				})
			}
		}
	}
}

// runStateTestComparison runs a state test through both Geth and Giga and compares results
func runStateTestComparison(t *testing.T, st *stJSON, post stPost) {
	blockTime := time.Now()

	// Build the transaction
	signedTx, sender := BuildTransaction(t, st, post)
	txBytes := EncodeTxForApp(t, signedTx)

	// --- Run with Geth (baseline) ---
	gethCtx := NewStateTestContext(t, blockTime, 1, ModeV2withOCC)
	gethCtx.SetupPreState(t, st.Pre)
	// Associate sender address
	senderSei := gethCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gethCtx.Ctx, sender)
	gethCtx.TestApp.EvmKeeper.SetAddressMapping(gethCtx.Ctx, senderSei, sender)

	_, gethResults, gethErr := RunStateTestBlock(t, gethCtx, [][]byte{txBytes})

	// --- Run with Giga ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, ModeGigaSequential)
	gigaCtx.SetupPreState(t, st.Pre)
	// Associate sender address
	senderSeiGiga := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, sender)
	gigaCtx.TestApp.EvmKeeper.SetAddressMapping(gigaCtx.Ctx, senderSeiGiga, sender)

	_, gigaResults, gigaErr := RunStateTestBlock(t, gigaCtx, [][]byte{txBytes})

	// --- Compare execution errors ---
	if gethErr != nil && gigaErr != nil {
		// Both failed - check if same type of failure
		t.Logf("Both executors failed: geth=%v, giga=%v", gethErr, gigaErr)
		return
	}
	if gethErr != nil {
		t.Fatalf("Geth execution failed but Giga succeeded: %v", gethErr)
	}
	if gigaErr != nil {
		t.Fatalf("Giga execution failed but Geth succeeded: %v", gigaErr)
	}

	// --- Compare results ---
	require.Equal(t, len(gethResults), len(gigaResults), "result count mismatch")

	for i := range gethResults {
		// Compare success/failure
		if gethResults[i].Code != gigaResults[i].Code {
			t.Logf("tx[%d] Geth: code=%d log=%q", i, gethResults[i].Code, gethResults[i].Log)
			t.Logf("tx[%d] Giga: code=%d log=%q", i, gigaResults[i].Code, gigaResults[i].Log)
		}
		require.Equal(t, gethResults[i].Code, gigaResults[i].Code,
			"tx[%d] result code mismatch", i)

		// Compare EvmTxInfo if present
		if gethResults[i].EvmTxInfo != nil && gigaResults[i].EvmTxInfo != nil {
			require.Equal(t, gethResults[i].EvmTxInfo.TxHash, gigaResults[i].EvmTxInfo.TxHash,
				"tx[%d] tx hash mismatch", i)
		}
	}
}

// TestGigaVsGeth_StateTest_Simple is a simple sanity test using embedded test data
func TestGigaVsGeth_StateTest_Simple(t *testing.T) {
	// A simple state test: transfer value from one account to another
	blockTime := time.Now()

	// Create a sender with funds
	senderKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(senderKey.PublicKey)

	// Create a receiver
	receiverKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	receiver := crypto.PubkeyToAddress(receiverKey.PublicKey)

	// Pre-state: sender has 1 ETH
	pre := ethtypes.GenesisAlloc{
		sender: {
			Balance: big.NewInt(1e18),
			Nonce:   0,
		},
	}

	// Create a simple value transfer transaction
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1e9),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1e15), // 0.001 ETH
		Data:     nil,
	})

	signer := ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID))
	signedTx, err := ethtypes.SignTx(tx, signer, senderKey)
	require.NoError(t, err)

	txBytes := EncodeTxForApp(t, signedTx)

	// --- Run with Geth ---
	gethCtx := NewStateTestContext(t, blockTime, 1, ModeV2withOCC)
	gethCtx.SetupPreState(t, pre)
	senderSei := gethCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gethCtx.Ctx, sender)
	gethCtx.TestApp.EvmKeeper.SetAddressMapping(gethCtx.Ctx, senderSei, sender)

	_, gethResults, gethErr := RunStateTestBlock(t, gethCtx, [][]byte{txBytes})
	require.NoError(t, gethErr, "Geth execution failed")
	require.Len(t, gethResults, 1)

	// --- Run with Giga ---
	gigaCtx := NewStateTestContext(t, blockTime, 1, ModeGigaSequential)
	gigaCtx.SetupPreState(t, pre)
	senderSeiGiga := gigaCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(gigaCtx.Ctx, sender)
	gigaCtx.TestApp.EvmKeeper.SetAddressMapping(gigaCtx.Ctx, senderSeiGiga, sender)

	_, gigaResults, gigaErr := RunStateTestBlock(t, gigaCtx, [][]byte{txBytes})
	require.NoError(t, gigaErr, "Giga execution failed")
	require.Len(t, gigaResults, 1)

	// --- Compare ---
	t.Logf("Geth result: code=%d, gasUsed=%d", gethResults[0].Code, gethResults[0].GasUsed)
	t.Logf("Giga result: code=%d, gasUsed=%d", gigaResults[0].Code, gigaResults[0].GasUsed)

	require.Equal(t, gethResults[0].Code, gigaResults[0].Code, "result code mismatch")
	require.Equal(t, uint32(0), gethResults[0].Code, "Geth tx failed: %s", gethResults[0].Log)
	require.Equal(t, uint32(0), gigaResults[0].Code, "Giga tx failed: %s", gigaResults[0].Log)

	t.Log("Simple state test passed: Giga matches Geth")
}
