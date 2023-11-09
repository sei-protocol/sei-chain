package evmrpc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "subscribe", "newHeads")

	// Start a goroutine to receive and print messages
	receivedMsg := false
	// timer for 5 seconds
	timer := time.NewTimer(5 * time.Second)

	for {
		select {
		case resObj := <-recvCh:
			receivedMsg = true
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			fmt.Println("Received message:", resObj)
		case <-timer.C:
			fmt.Println("Timer expired")
			if !receivedMsg {
				t.Fatal("No message received within 5 seconds")
			}
			done <- struct{}{}
			return
		}
	}
}

func TestSubscribe(t *testing.T) {
	manager := evmrpc.NewSubscriptionManager(&MockClient{})
	res, err := manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.Equal(t, 1, int(res))

	res, err = manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.Equal(t, 2, int(res))

	badManager := evmrpc.NewSubscriptionManager(&MockBadClient{})
	_, err = badManager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.NotNil(t, err)
}
