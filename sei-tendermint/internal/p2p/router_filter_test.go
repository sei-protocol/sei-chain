package p2p

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type fakeConn struct{}

func (fakeConn) Handshake(context.Context, types.NodeInfo, crypto.PrivKey) (types.NodeInfo, error) {
	return types.NodeInfo{}, nil
}

func (fakeConn) ReceiveMessage(ctx context.Context) (ChannelID, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

func (fakeConn) SendMessage(context.Context, ChannelID, []byte) error { return nil }
func (fakeConn) LocalEndpoint() Endpoint                              { return Endpoint{} }
func (fakeConn) RemoteEndpoint() Endpoint                             { return Endpoint{} }
func (fakeConn) Close() error                                         { return nil }
func (fakeConn) String() string                                       { return "fakeConn" }

func TestConnectionFiltering(t *testing.T) {
	ctx := t.Context()
	logger := log.NewNopLogger()

	filterByIPCount := 0
	router := &Router{
		logger:      logger,
		connTracker: newConnTracker(1, time.Second),
		options: RouterOptions{
			FilterPeerByIP: func(ctx context.Context, addr netip.AddrPort) error {
				filterByIPCount++
				return errors.New("mock")
			},
		},
	}
	require.Equal(t, 0, filterByIPCount)
	router.openConnection(ctx, fakeConn{}) // TODO: needs to be more realistic.
	require.Equal(t, 1, filterByIPCount)
}
