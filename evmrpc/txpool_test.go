package evmrpc_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTxPoolContent(t *testing.T) {
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"txpool_content\",\"params\":[],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)

	// check pending has 1 txn in it
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})
	pendingMap := resObj["pending"].(map[string]interface{})
	require.Equal(t, 1, len(pendingMap))

	// check that txn
	for fromAddr, txns := range pendingMap {
		for nonce, txn := range txns.(map[string]interface{}) {
			require.NotZero(t, nonce)
			tx := txn.(map[string]interface{})
			require.Nil(t, tx["blockNumber"])
			require.Nil(t, tx["blockHash"])
			require.Equal(t, tx["chainId"], "0xae3f3")
			require.Equal(t, strings.ToLower(tx["from"].(string)), strings.ToLower(fromAddr))
			require.NotZero(t, tx["v"])
			require.NotZero(t, tx["r"])
			require.NotZero(t, tx["s"])
		}
	}

	// check queued has nothing in it
	queuedMap := resObj["queued"].(map[string]interface{})
	require.Equal(t, 0, len(queuedMap))
}
