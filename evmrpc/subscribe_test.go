package evmrpc_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "newHeads")
	NewHeadsCalled <- struct{}{}
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(1 * time.Second)

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
		"excessBlobGas", "parentBeaconBlockRoot", "withdrawlsRoot", "baseFeePerGas",
		"withdrawalsRoot", "blobGasUsed",
	} {
		inapplicableKeys[key] = struct{}{}
	}
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
			// check all fields
			for _, key := range expectedKeys {
				if _, ok := resultMap[key]; !ok {
					t.Fatalf("%s is nil", key)
				}
				// check that applicable keys aren't all 0's
				if _, inapplicable := inapplicableKeys[key]; !inapplicable {
					if matched, err := regexp.MatchString("^0+$", fmt.Sprintf("%v", resultMap[key])); err != nil || matched {
						t.Fatalf("%s was unable to parse or expected non-zero value", key)
					}
				}
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
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
		case _ = <-recvCh:
			return
		case <-timer.C:
			t.Fatal("No message received within 5 seconds")
		}
	}
}

func TestSubscribeNewLogs(t *testing.T) {
	data := map[string]interface{}{
		"fromBlock": "0x0",
		"toBlock":   "latest",
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
			if resultMap["address"] != "0x1111111111111111111111111111111111111111" && resultMap["address"] != "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" {
				t.Fatalf("Unexpected address, got %v", resultMap["address"])
			}
			firstTopic := resultMap["topics"].([]interface{})[0].(string)
			if firstTopic != "0x1111111111111111111111111111111111111111111111111111111111111111" {
				t.Fatalf("Unexpected topic, got %v", firstTopic)
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
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
