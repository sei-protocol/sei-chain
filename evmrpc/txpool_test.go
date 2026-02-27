package evmrpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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
			require.Equal(t, strings.ToLower(tx["from"].(string)), strings.ToLower(fromAddr))
			requireNotZeroHex(t, tx["gas"].(string))
			requireNotZeroHex(t, tx["gasPrice"].(string))
			// maxFeePerGas
			requireNotZeroHex(t, tx["maxFeePerGas"].(string))
			// maxPriorityFeePerGas
			// hash
			requireNotZeroHex(t, tx["hash"].(string))
			// input
			requireNotZeroHex(t, tx["input"].(string))
			// nonce -- can be 0
			// to
			requireNotZeroHex(t, tx["to"].(string))
			// transactionIndex -- not set yet for pending
			// value
			requireNotZeroHex(t, tx["value"].(string))
			// type -- can be 0
			// acccesslist-- can be any array value
			require.Equal(t, tx["chainId"], "0xae3f2") // 713714
			requireNotZeroHex(t, tx["v"].(string))
			requireNotZeroHex(t, tx["r"].(string))
			requireNotZeroHex(t, tx["s"].(string))
		}
	}

	// check queued has nothing in it
	queuedMap := resObj["queued"].(map[string]interface{})
	require.Equal(t, 0, len(queuedMap))
}

func requireNotZeroHex(t *testing.T, hexStr string) {
	if strings.HasPrefix(hexStr, "0x") {
		hexStr = hexStr[2:]
	}
	for i := 0; i < len(hexStr); i++ {
		if hexStr[i] != '0' {
			return // not all zeros
		}
	}
	t.Errorf("requireNotZeroHex: %s is all zeros", hexStr)
}

func TestTxPoolContentSenderRecovery(t *testing.T) {
	ctxProvider := func(int64) sdk.Context { return Ctx }
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }

	api := evmrpc.NewTxPoolAPI(
		&MockClient{},
		EVMKeeper,
		ctxProvider,
		txConfigProvider,
		evmrpc.NewTxPoolConfig(10),
		evmrpc.ConnectionTypeHTTP,
	)

	content, err := api.Content(context.Background())
	require.NoError(t, err)

	pending := content["pending"]
	require.Len(t, pending, 1)
	require.Empty(t, content["queued"])

	ethMsg := evmtypes.MustGetEVMTransactionMessage(UnconfirmedTx)
	ethTx, _ := ethMsg.AsTransaction()

	expectedSender, err := rpcutils.RecoverEVMSenderWithContext(Ctx, ethTx)
	require.NoError(t, err)

	nonceStr := strconv.FormatUint(ethTx.Nonce(), 10)
	senderTxs, ok := pending[expectedSender.Hex()]
	if !ok {
		for addr := range pending {
			if strings.EqualFold(addr, expectedSender.Hex()) {
				senderTxs = pending[addr]
				ok = true
				break
			}
		}
	}
	require.True(t, ok, "pending txs keyed by expected sender not found")

	rpcTx, ok := senderTxs[nonceStr]
	require.True(t, ok)
	require.Equal(t, expectedSender, rpcTx.From)
	require.Equal(t, ethTx.Hash(), rpcTx.Hash)
}
