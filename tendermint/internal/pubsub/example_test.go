package pubsub_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/tendermint/internal/pubsub"
	"github.com/sei-protocol/sei-chain/tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/tendermint/libs/log"
)

func TestExample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestServer(ctx, t, log.NewNopLogger())

	sub := newTestSub(t).must(s.SubscribeWithArgs(ctx, pubsub.SubscribeArgs{
		ClientID: "example-client",
		Query:    query.MustCompile(`abci.account.name='John'`),
	}))

	events := []abci.Event{
		{
			Type:       "abci.account",
			Attributes: []abci.EventAttribute{{Key: []byte("name"), Value: []byte("John")}},
		},
	}
	require.NoError(t, s.PublishWithEvents(pubstring("Tombstone"), events))
	sub.mustReceive(ctx, pubstring("Tombstone"))
}
