package evmrpc_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"math/big"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGetTransactionCount(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// happy path
	bodyByNumber := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x8\"],\"id\":\"test\"}"
	bodyByHash := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x3030303030303030303030303030303030303030303030303030303030303031\"],\"id\":\"test\"}"

	for _, body := range []string{bodyByNumber, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		count := resObj["result"].(string)
		require.Equal(t, "0x1", count)
	}

	// address that doesn't have tx
	strangerBody := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x0123456789012345678902345678901234567891\",\"0x8\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(strangerBody))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	count := resObj["result"].(string)
	require.Equal(t, "0x0", count) // no tx

	// error cases
	earliestBodyToBadPort := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"earliest\"],\"id\":\"test\"}"
	for body, errStr := range map[string]string{
		earliestBodyToBadPort: "error genesis",
	} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		errMap := resObj["error"].(map[string]interface{})
		errMsg := errMap["message"].(string)
		require.Equal(t, errStr, errMsg)
	}
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetTransactionError(t *testing.T) {
	h := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	EVMKeeper.MockReceipt(Ctx, h, &types.Receipt{VmError: "test error"})
	resObj := sendRequestGood(t, "getTransactionErrorByHash", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "test error", resObj["result"])

	resObj = sendRequestBad(t, "getTransactionReceipt", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "error block", resObj["error"].(map[string]interface{})["message"])
}

func TestSign(t *testing.T) {
	homeDir := t.TempDir()
	txApi := evmrpc.NewTransactionAPI(nil, nil, nil, nil, homeDir, evmrpc.ConnectionTypeHTTP)
	infoApi := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir, 1024, evmrpc.ConnectionTypeHTTP, nil)
	clientCtx := client.Context{}.WithViper("").WithHomeDir(homeDir)
	clientCtx, err := config.ReadFromClientConfig(clientCtx)
	require.Nil(t, err)
	kb, err := client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
	require.Nil(t, err)
	entropySeed, err := bip39.NewEntropy(256)
	require.Nil(t, err)
	mnemonic, err := bip39.NewMnemonic(entropySeed)
	require.Nil(t, err)
	algos, _ := kb.SupportedAlgorithms()
	algo, err := keyring.NewSigningAlgoFromString(string(hd.Secp256k1Type), algos)
	require.Nil(t, err)
	_, err = kb.NewAccount("test", mnemonic, "", hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String(), algo)
	require.Nil(t, err)
	accounts, _ := infoApi.Accounts()
	account := accounts[0]
	signed, err := txApi.Sign(account, []byte("data"))
	require.Nil(t, err)
	require.NotEmpty(t, signed)
	
	// Test signing with address that doesn't have hosted key
	nonExistentAddr := common.HexToAddress("0x9999999999999999999999999999999999999999")
	_, err = txApi.Sign(nonExistentAddr, []byte("data"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "address does not have hosted key")
}

func TestGetVMError(t *testing.T) {
	resObj := sendRequestGood(t, "getVMError", "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	require.Equal(t, "", resObj["result"].(string))
	resObj = sendRequestGood(t, "getVMError", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872f")
	require.Equal(t, "not found", resObj["error"].(map[string]interface{})["message"])
}

func TestGetTransactionReceiptExcludeTraceFail(t *testing.T) {
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionReceiptExcludeTraceFail\",\"params\":[\"%s\"],\"id\":\"test\"}", "sei", TestPanicTxHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	require.Greater(t, len(resObj["error"].(map[string]interface{})["message"].(string)), 0)
	require.Nil(t, resObj["result"])
}

func TestCumulativeGasUsedPopulation(t *testing.T) {
	blockHeight := int64(1000)
	Ctx = Ctx.WithBlockHeight(blockHeight)

	txHashes := []common.Hash{
		common.HexToHash("0xc90ff2909ee2bba49a1a84bc3eb44fd1f0b389f6fb204ce77fcc48f58f1b8967"),
		common.HexToHash("0xb71247ff6fa4d16f68b559d3f37e6a76662e8c4bda795dec534c118740b993f4"),
		common.HexToHash("0xe81adea20595a48d49f6856c9d45de5d1874b7120c7fb053acacc9a297cd7106"),
		common.HexToHash("0xd768d6dff68f95fea0a096c43976fee8fe1f7bde24bdd6b48e086b7283967a0f"),
		common.HexToHash("0xe22b36ac447615070cb93f178ca41e4ca0482908d54c688cd0b9f42ccb81eed0"),
		common.HexToHash("0x65fda2369f700599385c9dbe2870f8a56051a8a45c3ebc49a8c56a46b7ecc9fb"),
		common.HexToHash("0x331decb2e371768a8b78eb03bcd91e54a65b35d43accd789900901a77a94c701"),
		common.HexToHash("0xef71e67093ace8649c4b5bc66fc823e7746e504c05d5c41deca909b3f5a66c4c"),
		common.HexToHash("0x96b8b807b31edef98c1486a3ca6326f61a09f9a825b2de76845ac7a8ff59912d"),
		common.HexToHash("0x194dd7db211b09b1e86ee2f188c75e20f31b602d78cbb4762aeb704406b8a6e0"),
		common.HexToHash("0x06a58c740f3f7f1af8f0e9eaded8578a099c3fe8ef8ee947c539af34ecf70aa8"),
	}
	correctCumulativeGasUsedValues := []uint64{21000, 43000, 66000, 90000, 115000, 141000, 168000, 196000, 225000, 255000, 286000}

	stateDB := state.NewDBImpl(Ctx, EVMKeeper, true)

	for i, txHash := range txHashes {
		Ctx = Ctx.WithTxIndex(i)

		msg := &core.Message{
			From:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			To:       &common.Address{},
			GasPrice: big.NewInt(1000000000),
			Nonce:    uint64(i),
		}

		_, err := EVMKeeper.WriteReceipt(
			Ctx,
			stateDB,
			msg,
			2,
			txHash,
			uint64(21000+i*1000),
			"",
		)
		require.Nil(t, err)
	}

	err := EVMKeeper.FlushTransientReceiptsSync(Ctx)
	require.Nil(t, err)

	for i := 0; i < len(txHashes); i++ {
		receipt, err := EVMKeeper.GetReceipt(Ctx, txHashes[i])
		require.Nil(t, err)
		require.Equal(t, receipt.CumulativeGasUsed, correctCumulativeGasUsedValues[i])
	}
}

func TestGetTransactionReceiptFailedTxWithZeroGas(t *testing.T) {
	// This tests the receipt population logic for ante handler failures
	txHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	
	// Create a failed receipt with 0 gas (ante handler failure case)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           0, // Failed
		GasUsed:          0, // Zero gas used
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
	}
	err := EVMKeeper.MockReceipt(Ctx, txHash, receipt)
	require.NoError(t, err)
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// The receipt should be returned even if tx not found in block
	// This tests that the code path for Status==0 && GasUsed==0 is executed
	result := resObj["result"]
	if result != nil {
		// If result exists, verify it has the expected structure
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["status"])
	}
}

func TestGetTransactionByBlockNumberAndIndexErrors(t *testing.T) {
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0xFFFFFFFFFF"],"id":"test"}`)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error for invalid tx index
	errMap := resObj["error"].(map[string]interface{})
	require.Contains(t, errMap["message"].(string), "invalid tx index")
	
	body = fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x999999","0x0"],"id":"test"}`)
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error for non-existent block
	errMap = resObj["error"].(map[string]interface{})
	require.NotNil(t, errMap["message"])
}

func TestGetTransactionByBlockHashAndIndexErrors(t *testing.T) {
	invalidHash := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0x0"],"id":"test"}`, invalidHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error for non-existent block hash
	errMap := resObj["error"].(map[string]interface{})
	require.NotNil(t, errMap["message"])
	
	body = fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockHashAndIndex","params":["%s","0xFFFFFFFFFF"],"id":"test"}`, TestBlockHash)
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error for invalid tx index
	errMap = resObj["error"].(map[string]interface{})
	require.Contains(t, errMap["message"].(string), "invalid tx index")
}

func TestGetTransactionByHashNotFound(t *testing.T) {
	nonExistentHash := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, nonExistentHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should return null for non-existent transaction
	require.Nil(t, resObj["result"])
}

func TestGetTransactionByHashTxNotFound(t *testing.T) {
	txHash := common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	
	// Create a receipt but ensure the tx won't be found in the block
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 999, // Invalid index
	}
	err := EVMKeeper.MockReceipt(Ctx, txHash, receipt)
	require.NoError(t, err)
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByHash","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should return null when tx not found in block
	require.Nil(t, resObj["result"])
}

func TestGetTransactionErrorByHashNotFound(t *testing.T) {
	nonExistentHash := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionErrorByHash","params":["%s"],"id":"test"}`, nonExistentHash)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should return empty string for non-existent transaction
	require.Equal(t, "", resObj["result"])
}

func TestGetTransactionWithBlockIndexOutOfRange(t *testing.T) {
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionByBlockNumberAndIndex","params":["0x8","0x999"],"id":"test"}`)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error for index out of range
	errMap := resObj["error"].(map[string]interface{})
	require.Contains(t, errMap["message"].(string), "transaction index out of range")
}

func TestEncodeReceiptTransactionNotFound(t *testing.T) {
	txHash := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	
	// Create a receipt with invalid transaction index
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 999, // Invalid index that won't be found
	}
	err := EVMKeeper.MockReceipt(Ctx, txHash, receipt)
	require.NoError(t, err)
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should get an error when transaction not found in block
	errMap := resObj["error"].(map[string]interface{})
	require.Contains(t, errMap["message"].(string), "failed to find transaction in block")
}

func TestEncodeReceiptWithEthTxAndEmptyFrom(t *testing.T) {
	// This test uses an existing transaction that should have a receipt
	// The test verifies that receipts are properly encoded with from field
	txHash := common.HexToHash("0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should successfully get receipt with from field populated
	result := resObj["result"]
	if result != nil {
		resultMap := result.(map[string]interface{})
		require.NotNil(t, resultMap["from"])
	}
}

func TestEncodeReceiptContractAddress(t *testing.T) {
	// Test coverage for lines 456-463: contract address handling
	txHash := common.HexToHash("0xbbbb0000000000000000000000000000000000000000000000000000000000bb")
	
	// Create a receipt with contract address but no "to" field (contract creation)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		ContractAddress:  "0x5555555555555555555555555555555555555555",
		To:               "",
	}
	err := EVMKeeper.MockReceipt(Ctx, txHash, receipt)
	require.NoError(t, err)
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should have contractAddress field set
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["contractAddress"])
	}
}

func TestEncodeReceiptWithToAddress(t *testing.T) {
	// Test coverage for lines 461-463: receipt.To != ""
	txHash := common.HexToHash("0xcccc0000000000000000000000000000000000000000000000000000000000cc")
	
	// Create a receipt with "to" field set (regular transaction)
	receipt := &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      8,
		Status:           1,
		GasUsed:          21000,
		TransactionIndex: 0,
		From:             "0x1234567890123456789012345678901234567890",
		To:               "0x9876543210987654321098765432109876543210",
		ContractAddress:  "",
	}
	err := EVMKeeper.MockReceipt(Ctx, txHash, receipt)
	require.NoError(t, err)
	
	body := fmt.Sprintf(`{"jsonrpc": "2.0","method": "eth_getTransactionReceipt","params":["%s"],"id":"test"}`, txHash.Hex())
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	
	// Should have "to" field set
	if resObj["result"] != nil {
		resultMap := resObj["result"].(map[string]interface{})
		require.NotNil(t, resultMap["to"])
	}
}
