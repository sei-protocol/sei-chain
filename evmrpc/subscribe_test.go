package evmrpc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestNewSubscribeAPI(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "subscribe", "newHeads")

	// Start a goroutine to receive and print messages
	for {
		select {
		case msg := <-recvCh:
			fmt.Println("Received message:", msg)
			continue
		case <-time.After(10 * time.Second):
			fmt.Println("No message received within 10 seconds")
			t.Fatal("No message received within 10 seconds")
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
