package evmrpc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(1 * time.Second)

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
			// check some basic stuff like number and transactionRoot
			if resultMap["number"] == nil {
				t.Fatal("Block number is nil")
			}
			if resultMap["transactionsRoot"] == nil {
				t.Fatal("Transaction root is nil")
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
		}
	}
}

func TestSubscribeNewLogs(t *testing.T) {
	t.Parallel()
	// data := "{\"address\":\"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48\",\"topics\":[\"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef\"]}"
	data := map[string]interface{}{
		"address": []common.Address{
			common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
		},
		"topics": [][]common.Hash{
			{
				common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123"),
			},
		},
	}
	// data := map[string]interface{}{}
	recvCh, done := sendWSRequestGood(t, "subscribe", "subscribe", "logs", data)
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(2 * time.Second)

	var subscriptionId string

	for {
		select {
		case resObj := <-recvCh:
			fmt.Println("resObj:", resObj)
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
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
		}
	}
}

// TODO: test unsubscribe somehow (maybe just test within subscribe)

func TestSubscribe(t *testing.T) {
	manager := evmrpc.NewSubscriptionManager(&MockClient{})
	res, subCh, err := manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 1, int(res))

	res, subCh, err = manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 2, int(res))

	badManager := evmrpc.NewSubscriptionManager(&MockBadClient{})
	_, subCh, err = badManager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.NotNil(t, err)
	require.Nil(t, subCh)
}
