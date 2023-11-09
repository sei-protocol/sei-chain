package evmrpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	// timer for 5 seconds
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
			resMap := paramMap["result"].(map[string]interface{})
			query := resMap["query"].(string)
			if query != "tm.event = 'NewBlockHeader'" {
				t.Fatal("query is not correct")
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
		}
	}
}

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
