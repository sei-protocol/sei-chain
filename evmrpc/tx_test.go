package evmrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGetTxReceipt(t *testing.T) {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)

	hash := common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234")
	require.Nil(t, EVMKeeper.SetReceipt(Ctx, hash, &types.Receipt{
		From:              "0x123456789012345678902345678901234567890",
		To:                "0x123456789012345678902345678901234567890",
		TransactionIndex:  0,
		BlockNumber:       8,
		TxType:            1,
		ContractAddress:   "0x123456789012345678902345678901234567890",
		CumulativeGasUsed: 123,
		TxHashHex:         "0x123456789012345678902345678901234567890123456789012345678901234",
		GasUsed:           55,
		Status:            0,
		EffectiveGasPrice: 10,
	}))

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionReceipt\",\"params\":[\"0x1234567890123456789012345678901234567890123456789012345678901234\"],\"id\":\"test\"}"
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
	require.Equal(t, "0x3030303030303030303030303030303030303030303030303030303030303031", resObj["blockHash"].(string))
	require.Equal(t, "0x8", resObj["blockNumber"].(string))
	require.Equal(t, "0x0123456789012345678902345678901234567890", resObj["contractAddress"].(string))
	require.Equal(t, "0x7b", resObj["cumulativeGasUsed"].(string))
	require.Equal(t, "0xa", resObj["effectiveGasPrice"].(string))
	require.Equal(t, "0x0123456789012345678902345678901234567890", resObj["from"].(string))
	require.Equal(t, "0x37", resObj["gasUsed"].(string))
	logs := resObj["logs"].([]interface{})
	require.Equal(t, 1, len(logs))
	log := logs[0].(map[string]interface{})
	require.Equal(t, "0x1111111111111111111111111111111111111111", log["address"].(string))
	topics := log["topics"].([]interface{})
	require.Equal(t, 2, len(topics))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", topics[0].(string))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111112", topics[1].(string))
	require.Equal(t, "0x78797a", log["data"].(string))
	require.Equal(t, "0x8", log["blockNumber"].(string))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111113", log["transactionHash"].(string))
	require.Equal(t, "0x2", log["transactionIndex"].(string))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", log["blockHash"].(string))
	require.Equal(t, "0x1", log["logIndex"].(string))
	require.True(t, log["removed"].(bool))
	require.Equal(t, "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", resObj["logsBloom"].(string))
	require.Equal(t, "0x0", resObj["status"].(string))
	require.Equal(t, "0x0123456789012345678902345678901234567890", resObj["to"].(string))
	require.Equal(t, "0x0123456789012345678902345678901234567890123456789012345678901234", resObj["transactionHash"].(string))
	require.Equal(t, "0x0", resObj["transactionIndex"].(string))
	require.Equal(t, "0x1", resObj["type"].(string))

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
}
