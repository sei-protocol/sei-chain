package helpers

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

// Test helper to create a signed transaction
func createSignedTx(t *testing.T, chainID *big.Int, txType uint8, nonce uint64) (*types.Transaction, common.Address) {
	// Generate a test private key
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Get the address from the private key
	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Create transaction data
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	value := big.NewInt(1000000000000000000) // 1 ETH
	gasLimit := uint64(21000)
	gasPrice := big.NewInt(1000000000) // 1 Gwei
	data := []byte{}

	var tx *types.Transaction

	switch txType {
	case types.LegacyTxType:
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       &to,
			Value:    value,
			Data:     data,
		})
	case types.AccessListTxType:
		tx = types.NewTx(&types.AccessListTx{
			ChainID:  chainID,
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       &to,
			Value:    value,
			Data:     data,
		})
	case types.DynamicFeeTxType:
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(2000000000),
			Gas:       gasLimit,
			To:        &to,
			Value:     value,
			Data:      data,
		})
	default:
		t.Fatalf("Unsupported transaction type: %d", txType)
	}

	// Sign the transaction
	// For chain ID 0, we need to use the same signer logic as our recovery function
	var signer types.Signer
	if chainID.Cmp(big.NewInt(0)) == 0 {
		// For chain ID 0, use London signer (matches our recovery logic)
		signer = types.NewLondonSigner(chainID)
	} else {
		signer = types.LatestSignerForChainID(chainID)
	}
	signedTx, err := types.SignTx(tx, signer, privateKey)
	require.NoError(t, err)

	return signedTx, expectedAddr
}

func TestRecoverEVMSender_LegacyTx_ChainID1329(t *testing.T) {
	chainID := big.NewInt(1329)
	tx, expectedAddr := createSignedTx(t, chainID, types.LegacyTxType, 0)

	recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match expected")
}

func TestRecoverEVMSender_LegacyTx_ChainID1(t *testing.T) {
	chainID := big.NewInt(1)
	tx, expectedAddr := createSignedTx(t, chainID, types.LegacyTxType, 0)

	recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match expected for chain ID 1")
}

func TestRecoverEVMSender_AccessListTx(t *testing.T) {
	chainID := big.NewInt(1329)
	tx, expectedAddr := createSignedTx(t, chainID, types.AccessListTxType, 0)

	recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match expected for AccessList tx")
}

func TestRecoverEVMSender_DynamicFeeTx(t *testing.T) {
	chainID := big.NewInt(1329)
	tx, expectedAddr := createSignedTx(t, chainID, types.DynamicFeeTxType, 0)

	recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match expected for DynamicFee tx")
}

func TestRecoverEVMSender_DifferentNonces(t *testing.T) {
	chainID := big.NewInt(1329)

	// Test with different nonces to ensure nonce doesn't affect recovery
	for nonce := uint64(0); nonce < 10; nonce++ {
		tx, expectedAddr := createSignedTx(t, chainID, types.LegacyTxType, nonce)

		recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
		require.NoError(t, err)
		require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for nonce %d", nonce)
	}
}

func TestRecoverEVMSender_MultipleChainIDs(t *testing.T) {
	// Test various chain IDs (excluding 0 - see note below)
	chainIDs := []*big.Int{
		big.NewInt(1),
		big.NewInt(1329),      // Pacific-1
		big.NewInt(713715),    // Arctic-1
		big.NewInt(531050104), // Atlantic-2
	}

	for _, chainID := range chainIDs {
		t.Run("ChainID_"+chainID.String(), func(t *testing.T) {
			tx, expectedAddr := createSignedTx(t, chainID, types.LegacyTxType, 0)

			recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
			require.NoError(t, err)
			require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for chain ID %s", chainID.String())
		})
	}

	// Note: Chain ID 0 is tested with real blockchain data in TestRecoverEVMSender_FromTestData
	// Synthetic chain ID 0 transactions don't match real-world signing behavior
}

func TestRecoverEVMSender_AllTxTypes(t *testing.T) {
	chainID := big.NewInt(1329)
	txTypes := []uint8{
		types.LegacyTxType,
		types.AccessListTxType,
		types.DynamicFeeTxType,
	}

	for _, txType := range txTypes {
		t.Run("TxType_"+string(rune(txType+'0')), func(t *testing.T) {
			tx, expectedAddr := createSignedTx(t, chainID, txType, 0)

			recoveredAddr, err := RecoverEVMSender(tx, 1000000, 1234567890)
			require.NoError(t, err)
			require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for tx type %d", txType)
		})
	}
}

func TestRecoverEVMSender_ContractCreation(t *testing.T) {
	// Test contract creation transaction (no To address)
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	chainID := big.NewInt(1329)

	// Create contract creation tx (To is nil)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      1000000,
		To:       nil, // Contract creation
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x80, 0x60, 0x40}, // Some bytecode
	})

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	require.NoError(t, err)

	recoveredAddr, err := RecoverEVMSender(signedTx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for contract creation")
}

func TestRecoverEVMSender_DifferentBlockHeights(t *testing.T) {
	chainID := big.NewInt(1329)
	tx, expectedAddr := createSignedTx(t, chainID, types.LegacyTxType, 0)

	// Test with different block heights (should not affect recovery)
	blockHeights := []int64{1, 1000, 1000000, 100000000, 170818561}

	for _, height := range blockHeights {
		recoveredAddr, err := RecoverEVMSender(tx, height, 1234567890)
		require.NoError(t, err)
		require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for block height %d", height)
	}
}

func TestRecoverEVMSender_UnprotectedLegacyTx(t *testing.T) {
	// Create an unprotected legacy transaction (pre-EIP-155)
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000000000000000000),
		Data:     []byte{},
	})

	// Sign with HomesteadSigner (unprotected)
	signer := types.HomesteadSigner{}
	signedTx, err := types.SignTx(tx, signer, privateKey)
	require.NoError(t, err)

	recoveredAddr, err := RecoverEVMSender(signedTx, 1000000, 1234567890)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, recoveredAddr, "Recovered address should match for unprotected legacy tx")
}

func TestRecoverEVMSender_RealWorldTransaction(t *testing.T) {
	// This is the actual transaction from block 170818561 that we debugged
	to := common.HexToAddress("0xa26b9bfe606d29f16b5aecf30f9233934452c4e2")
	gasPrice := big.NewInt(3987777747)
	value := big.NewInt(0)
	data := common.Hex2Bytes("a0712d68000000000000000000000000000000000000000000000000000000003b9aca00")
	v := big.NewInt(35)
	r, _ := new(big.Int).SetString("8774931ee5b3fed1eafb67cc3e38202265381f5aaebb2ca7fa8ff679068f0337", 16)
	s, _ := new(big.Int).SetString("2a07e49c9f0bbaea59534553003e33fb476169072f6f18872983872b3e447370", 16)

	ethTx := types.NewTx(&types.LegacyTx{
		Nonce:    7,
		GasPrice: gasPrice,
		Gas:      500000,
		To:       &to,
		Value:    value,
		Data:     data,
		V:        v,
		R:        r,
		S:        s,
	})

	expectedSender := common.HexToAddress("0x07fF2517E630c1CEa9cC1eC594957cC293aa80B2")

	recoveredAddr, err := RecoverEVMSender(ethTx, 170818561, 1727667341)
	require.NoError(t, err)
	require.Equal(t, expectedSender, recoveredAddr, "Should recover the correct sender for real-world tx")
}

// TestRecoverEVMSender_GeneratedTransactions generates 1000 fresh transactions and validates recovery
func TestRecoverEVMSender_GeneratedTransactions(t *testing.T) {
	chainID := big.NewInt(1329)
	blockHeight := int64(1000000)
	blockTime := uint64(1234567890)

	successCount := 0
	failCount := 0

	// Test 500 AccessList (Type 1) transactions
	for i := 0; i < 500; i++ {
		// Generate a new private key
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

		// Create AccessList transaction
		to := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tx := types.NewTx(&types.AccessListTx{
			ChainID:  chainID,
			Nonce:    uint64(i),
			GasPrice: big.NewInt(1000000000),
			Gas:      21000,
			To:       &to,
			Value:    big.NewInt(1000000000000000000),
			Data:     []byte{},
		})

		// Sign the transaction
		signer := types.LatestSignerForChainID(chainID)
		signedTx, err := types.SignTx(tx, signer, privateKey)
		require.NoError(t, err)

		// Recover sender
		recoveredAddr, err := RecoverEVMSender(signedTx, blockHeight, blockTime)
		if err != nil {
			t.Errorf("Type 1 tx %d: Recovery failed: %v", i, err)
			failCount++
			continue
		}

		if recoveredAddr != expectedAddr {
			t.Errorf("Type 1 tx %d: Address mismatch! Expected %s, got %s",
				i, expectedAddr.Hex(), recoveredAddr.Hex())
			failCount++
		} else {
			successCount++
		}
	}

	// Test 500 DynamicFee (Type 2) transactions
	for i := 0; i < 500; i++ {
		// Generate a new private key
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

		// Create DynamicFee transaction
		to := common.HexToAddress("0x1234567890123456789012345678901234567890")
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     uint64(i),
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(2000000000),
			Gas:       21000,
			To:        &to,
			Value:     big.NewInt(1000000000000000000),
			Data:      []byte{},
		})

		// Sign the transaction
		signer := types.LatestSignerForChainID(chainID)
		signedTx, err := types.SignTx(tx, signer, privateKey)
		require.NoError(t, err)

		// Recover sender
		recoveredAddr, err := RecoverEVMSender(signedTx, blockHeight, blockTime)
		if err != nil {
			t.Errorf("Type 2 tx %d: Recovery failed: %v", i, err)
			failCount++
			continue
		}

		if recoveredAddr != expectedAddr {
			t.Errorf("Type 2 tx %d: Address mismatch! Expected %s, got %s",
				i, expectedAddr.Hex(), recoveredAddr.Hex())
			failCount++
		} else {
			successCount++
		}
	}

	t.Logf("\n=== Generated Transaction Test Summary ===")
	t.Logf("Total transactions: 1000 (500 Type 1 + 500 Type 2)")
	t.Logf("✅ Successful: %d", successCount)
	t.Logf("❌ Failed: %d", failCount)

	require.Equal(t, 1000, successCount, "All 1000 generated transactions should recover correctly")
	require.Equal(t, 0, failCount, "No transactions should fail recovery")
}

// TestRecoverEVMSender_FromTestData validates recovery against real transactions from testdata
func TestRecoverEVMSender_FromTestData(t *testing.T) {
	// Load test data from JSON file
	testDataFile := "testdata/transaction_test_data.json"
	data, err := os.ReadFile(testDataFile)
	if err != nil {
		t.Skipf("Skipping test - testdata file not found: %v", err)
		return
	}

	type TestTxData struct {
		TxHash       string `json:"tx_hash"`
		TxRLP        string `json:"tx_rlp"`
		ExpectedFrom string `json:"expected_from"`
		BlockNumber  uint64 `json:"block_number"`
		BlockTime    uint64 `json:"block_time"`
		TxType       uint8  `json:"tx_type"`
		ChainID      string `json:"chain_id"`
	}

	var testTxs []TestTxData
	err = json.Unmarshal(data, &testTxs)
	require.NoError(t, err)

	successCount := 0
	failCount := 0

	for i, txData := range testTxs {
		// Decode RLP
		txBytes, err := hex.DecodeString(txData.TxRLP)
		if err != nil {
			t.Errorf("Test %d: Failed to decode RLP: %v", i, err)
			failCount++
			continue
		}

		var tx types.Transaction
		err = rlp.DecodeBytes(txBytes, &tx)
		if err != nil {
			t.Errorf("Test %d: Failed to decode transaction: %v", i, err)
			failCount++
			continue
		}

		// Recover sender
		recoveredAddr, err := RecoverEVMSender(&tx, int64(txData.BlockNumber), txData.BlockTime)
		if err != nil {
			t.Errorf("Test %d (tx %s): Recovery failed: %v", i, txData.TxHash, err)
			failCount++
			continue
		}

		// Compare addresses (case-insensitive)
		expectedLower := strings.ToLower(txData.ExpectedFrom)
		recoveredLower := strings.ToLower(recoveredAddr.Hex())

		if expectedLower != recoveredLower {
			t.Errorf("Test %d (tx %s): Address mismatch!\n  Expected: %s\n  Got:      %s\n  ChainID: %s, Type: %d",
				i, txData.TxHash, txData.ExpectedFrom, recoveredAddr.Hex(), txData.ChainID, txData.TxType)
			failCount++
		} else {
			successCount++
		}
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Total transactions: %d", len(testTxs))
	t.Logf("✅ Successful: %d", successCount)
	t.Logf("❌ Failed: %d", failCount)

	if failCount > 0 {
		t.Fatalf("Failed to recover %d out of %d transactions", failCount, len(testTxs))
	}
}
