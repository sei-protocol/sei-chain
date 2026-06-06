package evmrpc_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}
	inapplicableKeys := make(map[string]struct{})
	for _, key := range []string{
		"difficulty", "extraData", "logsBloom", "nonce", "sha3Uncles", "mixHash",
		"excessBlobGas", "parentBeaconBlockRoot", "withdrawlsRoot",
		"withdrawalsRoot", "blobGasUsed",
	} {
		inapplicableKeys[key] = struct{}{}
	}
	var subscriptionId string
	allZerosRE := regexp.MustCompile(`^0+$`)
	for t.Context().Err() == nil {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if subscriptionId == "" {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				// Reset timer now that subscription is confirmed, then trigger new heads
				timer.Reset(5 * time.Second)
				NewHeadsCalled <- struct{}{}
				continue
			}
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			// check all fields
			for _, key := range expectedKeys {
				if _, ok := resultMap[key]; !ok {
					t.Fatalf("%s is nil", key)
				}
				// check that applicable keys aren't all 0's
				if _, inapplicable := inapplicableKeys[key]; !inapplicable {
					got := fmt.Sprintf("%v", resultMap[key])
					if allZerosRE.MatchString(got) {
						t.Fatalf("%s must be non-zero (got %v)", key, resultMap[key])
					}
				}
			}
			// Event validated successfully, no need to wait further
			return
		case <-timer.C:
			t.Fatal("No event received within 5 seconds")
		}
	}
}

// TestSubscribeNewHeadsAutobahn exercises the in-process notifier path
// (used under Autobahn) end-to-end: a WS client subscribes to newHeads
// against the notifier-backed server, the test pushes an event via
// NotifierForTest.OnBlockCommitted, and the WS subscriber must receive an
// eth_subscription notification carrying the encoded header.
func TestSubscribeNewHeadsAutobahn(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestAndListen(t, TestNotifierWSPort, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	hash := common.HexToHash("0x4242424242424242424242424242424242424242424242424242424242424242").Bytes()
	appHash := common.HexToHash("0x3131313131313131313131313131313131313131313131313131313131313131").Bytes()
	proposer := common.HexToAddress("0x9999999999999999999999999999999999999999").Bytes()
	ts := time.Unix(1_700_000_500, 0).UTC()

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	var subscriptionId string
	for t.Context().Err() == nil {
		select {
		case resObj := <-recvCh:
			if errVal, ok := resObj["error"]; ok {
				t.Fatal("Received error:", errVal)
			}
			if subscriptionId == "" {
				subscriptionId = resObj["result"].(string)
				timer.Reset(5 * time.Second)
				NotifierForTest.OnBlockCommitted(hash, &tmproto.Header{
					Height:          MockHeight8,
					Time:            ts,
					ProposerAddress: proposer,
				}, &abci.ResponseFinalizeBlock{
					AppHash: appHash,
					TxResults: []*abci.ExecTxResult{
						{GasUsed: 21000},
						{GasUsed: 50000},
					},
					// gasLimit is sourced from the SDK ConsensusParams in
					// the consumer goroutine, not from the response, so
					// ConsensusParamUpdates is intentionally omitted here.
				})
				continue
			}
			require.Equal(t, "eth_subscription", resObj["method"])
			paramMap := resObj["params"].(map[string]interface{})
			require.Equal(t, subscriptionId, paramMap["subscription"])
			resultMap := paramMap["result"].(map[string]interface{})
			for _, k := range expectedKeys {
				_, ok := resultMap[k]
				require.Truef(t, ok, "missing key %q in header", k)
			}
			require.Equal(t, common.BytesToHash(hash).Hex(), resultMap["hash"])
			require.Equal(t, fmt.Sprintf("0x%x", MockHeight8), resultMap["number"])
			require.Equal(t, common.BytesToAddress(proposer).Hex(), resultMap["miner"])
			require.Equal(t, common.BytesToHash(appHash).Hex(), resultMap["stateRoot"])
			require.Equal(t, fmt.Sprintf("0x%x", ts.Unix()), resultMap["timestamp"])
			require.Equal(t, fmt.Sprintf("0x%x", 21000+50000), resultMap["gasUsed"])
			// gasLimit comes from the SDK ConsensusParams that the test
			// runtime sets; just assert it's a non-zero hex string.
			gasLimitStr, _ := resultMap["gasLimit"].(string)
			require.NotEmpty(t, gasLimitStr)
			require.NotEqual(t, "0x0", gasLimitStr)
			// Fields the Autobahn path does not surface must serialize to
			// the zero hash.
			zeroHash := (common.Hash{}).Hex()
			require.Equal(t, zeroHash, resultMap["parentHash"])
			require.Equal(t, zeroHash, resultMap["receiptsRoot"])
			require.Equal(t, zeroHash, resultMap["transactionsRoot"])
			return
		case <-timer.C:
			t.Fatal("No event received within 5 seconds")
		}
	}
}

func TestSubscribeEmptyLogs(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(2 * time.Second)

	// just testing to see that we don't crash when no params are provided
	for {
		select {
		case <-recvCh:
			return
		case <-timer.C:
			t.Fatal("No message received within 5 seconds")
		}
	}
}

func TestSubscribeNewLogs(t *testing.T) {
	// Query MockHeight8 directly: the mocks place a matching log there
	// (tx1's receipt with address 0x1111…1111 and topic 0x1111…1111).
	// Using a fixed historical range avoids the FromBlock=0+ToBlock=latest
	// rewrite path so this test stays decoupled from "latest" placement;
	// TestSubscribeEmptyLogs covers the empty-filter handshake path.
	data := map[string]interface{}{
		"fromBlock": fmt.Sprintf("0x%x", MockHeight8),
		"toBlock":   fmt.Sprintf("0x%x", MockHeight8),
		"address": []common.Address{
			common.HexToAddress("0x1111111111111111111111111111111111111111"),
		},
		"topics": [][]common.Hash{
			{
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111112"),
			},
		},
	}
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs", data)
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(2 * time.Second)

	var subscriptionId string

	for {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if !receivedSubMsg {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				receivedSubMsg = true
				continue
			}
			receivedEvents = true
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			require.Equal(t, "0x1111111111111111111111111111111111111111", resultMap["address"])
			firstTopic := resultMap["topics"].([]interface{})[0].(string)
			require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", firstTopic)
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No subscription ack or log notification within 2 seconds")
			}
			return
		}
	}
}

func TestSubscriptionManager(t *testing.T) {
	manager := evmrpc.NewSubscriptionManager(&MockClient{})
	res, subCh, err := manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 1, int(res))

	res, subCh, err = manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 2, int(res))

	badManager := evmrpc.NewSubscriptionManager(&MockBadClient{})
	_, subCh, err = badManager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.NotNil(t, err)
	require.Nil(t, subCh)
}
