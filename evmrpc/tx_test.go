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

func TestGetTxReceipt(t *testing.T) {
	testGetTxReceipt(t, "eth")
}

func testGetTxReceipt(t *testing.T, namespace string) {
	receipt, err := EVMKeeper.GetReceipt(Ctx, common.HexToHash("0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32"))
	require.Nil(t, err)
	receipt.To = ""
	EVMKeeper.MockReceipt(Ctx, common.HexToHash("0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32"), receipt)

	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionReceipt\",\"params\":[\"0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32\"],\"id\":\"test\"}", namespace)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["blockHash"].(string))
	require.Equal(t, "0x8", resObj["blockNumber"].(string))
	require.Equal(t, "0x38", resObj["cumulativeGasUsed"].(string))
	require.Equal(t, "0x174876e800", resObj["effectiveGasPrice"].(string))
	require.Equal(t, "0x1234567890123456789012345678901234567890", resObj["from"].(string))
	require.Equal(t, "0x38", resObj["gasUsed"].(string))
	logs := resObj["logs"].([]interface{})
	require.Equal(t, 1, len(logs))
	log := logs[0].(map[string]interface{})
	require.Equal(t, "0x1111111111111111111111111111111111111111", log["address"].(string))
	topics := log["topics"].([]interface{})
	require.Equal(t, 2, len(topics))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", topics[0].(string))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111112", topics[1].(string))
	require.Equal(t, "0x8", log["blockNumber"].(string))
	require.Equal(t, "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32", log["transactionHash"].(string))
	require.Equal(t, "0x0", log["transactionIndex"].(string))
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", log["blockHash"].(string))
	require.Equal(t, "0x0", log["logIndex"].(string))
	require.False(t, log["removed"].(bool))
	require.Equal(t, "0x0", resObj["status"].(string))
	require.Equal(t, nil, resObj["to"])
	require.Equal(t, "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32", resObj["transactionHash"].(string))
	require.Equal(t, "0x0", resObj["transactionIndex"].(string))
	require.Equal(t, "0x1", resObj["type"].(string))
	require.Equal(t, "0x1234567890123456789012345678901234567890", resObj["contractAddress"].(string))

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["error"].(map[string]interface{})
	require.Equal(t, float64(-32000), resObj["code"].(float64))
	require.Equal(t, "error block", resObj["message"].(string))

	resObj = sendRequestGood(t, "getTransactionReceipt", common.HexToHash("0x3030303030303030303030303030303030303030303030303030303030303031"))
	require.Nil(t, resObj["result"])
}

func TestGetTransaction(t *testing.T) {
	testGetTransaction(t, "eth")
}

func testGetTransaction(t *testing.T, namespace string) {
	bodyByBlockNumberAndIndex := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByBlockNumberAndIndex\",\"params\":[\"0x8\",\"0x0\"],\"id\":\"test\"}", namespace)
	bodyByBlockHashAndIndex := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByBlockHashAndIndex\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"0x0\"],\"id\":\"test\"}", namespace)
	bodyByHash := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByHash\",\"params\":[\"0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32\"],\"id\":\"test\"}", namespace)
	for _, body := range []string{bodyByBlockNumberAndIndex, bodyByBlockHashAndIndex, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		resObj = resObj["result"].(map[string]interface{})
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["blockHash"].(string))
		require.Equal(t, "0x8", resObj["blockNumber"].(string))
		require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", resObj["from"].(string))
		require.Equal(t, "0x3e8", resObj["gas"].(string))
		require.Equal(t, "0xa", resObj["gasPrice"].(string))
		require.Equal(t, "0xa", resObj["maxFeePerGas"].(string))
		require.Equal(t, "0x0", resObj["maxPriorityFeePerGas"].(string))
		require.Equal(t, "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32", resObj["hash"].(string))
		require.Equal(t, "0x616263", resObj["input"].(string))
		require.Equal(t, "0x1", resObj["nonce"].(string))
		require.Equal(t, "0x0000000000000000000000000000000000010203", resObj["to"].(string))
		require.Equal(t, "0x0", resObj["transactionIndex"].(string))
		require.Equal(t, "0x3e8", resObj["value"].(string))
		require.Equal(t, "0x2", resObj["type"].(string))
		require.Equal(t, 0, len(resObj["accessList"].([]interface{})))
		require.Equal(t, "0xae3f2", resObj["chainId"].(string))
		require.Equal(t, "0x1", resObj["v"].(string))
		require.Equal(t, "0x2d9ec6f4c4ff4ab0ca8de6248f939e873d2aa9cb6156fa9368e34708dfb6c123", resObj["r"].(string))
		require.Equal(t, "0x35990bec00913db3cecd7f132b45c289280f4182751dab1c9c5ca609939319cb", resObj["s"].(string))
		require.Equal(t, "0x1", resObj["yParity"].(string))
	}

	for _, body := range []string{bodyByBlockNumberAndIndex, bodyByBlockHashAndIndex, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		require.Nil(t, resObj["result"])
	}
}

func TestGetPendingTransactionByHash(t *testing.T) {
	resObj := sendRequestGood(t, "getTransactionByHash", "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x1", result["nonce"])
	require.Equal(t, "0x2", result["type"].(string))
}

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
}

func TestGetVMError(t *testing.T) {
	resObj := sendRequestGood(t, "getVMError", "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	require.Equal(t, "", resObj["result"].(string))
	resObj = sendRequestGood(t, "getVMError", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872f")
	require.Equal(t, "not found", resObj["error"].(map[string]interface{})["message"])
}

func TestGetTransactionReceiptFailedTx(t *testing.T) {
	fromAddr := "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e" // Use the actual address from the block

	// Create a failed receipt with 0 gas used for a contract creation transaction
	failedReceipt := &types.Receipt{
		Status:           0, // failed status
		GasUsed:          0,
		BlockNumber:      8,
		TransactionIndex: 0,
		TxHashHex:        "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32",
		From:             fromAddr, // Use the actual from address
	}

	// Mock the receipt in the keeper
	txHash := common.HexToHash("0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	EVMKeeper.MockReceipt(Ctx, txHash, failedReceipt)

	// Create JSON-RPC request
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionReceipt\",\"params\":[\"0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32\"],\"id\":\"test\"}"

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)

	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)

	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})

	// Verify the receipt was filled with correct information for failed tx
	require.Equal(t, "0x0", resObj["status"].(string))  // Failed status
	require.Equal(t, "0x0", resObj["gasUsed"].(string)) // 0 gas used
	require.Equal(t, "0x8", resObj["blockNumber"].(string))
	require.Equal(t, "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32", resObj["transactionHash"].(string))
	require.Equal(t, fromAddr, resObj["from"].(string))

	// For contract creation transaction
	require.Equal(t, "0x0000000000000000000000000000000000010203", resObj["to"].(string))
	require.Nil(t, resObj["contractAddress"])
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

	for i := 0 ; i < len(txHashes); i++ {
		receipt, err := EVMKeeper.GetReceipt(Ctx, txHashes[i])
		require.Nil(t, err)
		require.Equal(t, receipt.CumulativeGasUsed,  correctCumulativeGasUsedValues[i])
	}
}

func TestTransactionIndexResponseConsistency(t *testing.T) {
	txHash := multiTxBlockTx2.Hash()
	blockHash := MultiTxBlockHash
	txIndex := "0x3"

	receiptResult := sendRequestGood(t, "getTransactionReceipt", txHash.Hex())
	require.NotNil(t, receiptResult["result"])
	receipt := receiptResult["result"].(map[string]interface{})
	receiptTxIndex := receipt["transactionIndex"].(string)

	txResult := sendRequestGood(t, "getTransactionByHash", txHash.Hex())
	require.NotNil(t, txResult["result"])
	tx := txResult["result"].(map[string]interface{})
	txIndexFromHash := tx["transactionIndex"].(string)

	blockHashAndIndexResult := sendRequestGood(t, "getTransactionByBlockHashAndIndex", blockHash, txIndex)
	require.NotNil(t, blockHashAndIndexResult["result"])
	txFromBlockHashAndIndex := blockHashAndIndexResult["result"].(map[string]interface{})
	txIndexFromBlockHashAndIndex := txFromBlockHashAndIndex["transactionIndex"].(string)

	require.Equal(t, receiptTxIndex, txIndexFromHash,
		"Transaction index should be the same between eth_getTransactionReceipt and eth_getTransactionByHash")
	require.Equal(t, receiptTxIndex, txIndexFromBlockHashAndIndex,
		"Transaction index should be the same between eth_getTransactionReceipt and eth_getTransactionByBlockHashAndIndex")
	require.Equal(t, txIndexFromHash, txIndexFromBlockHashAndIndex,
		"Transaction index should be the same between eth_getTransactionByHash and eth_getTransactionByBlockHashAndIndex")
}

