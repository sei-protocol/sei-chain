package p2p

import (
	"context"
	"io"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// The node info exchange is part of the handshake and holds an accept-semaphore
// slot, so it has to run under the handshake deadline. A peer that goes quiet
// part way through must be hung up on rather than left holding the slot.
func TestRouter_InboundNodeInfoBoundedByHandshakeDeadline(t *testing.T) {
	ctx := t.Context()

	privKey := NodeSecretKey(ed25519.GenerateSecretKey())
	endpoint := Endpoint{AddrPort: tcp.TestReserveAddr()}
	nodeInfo := types.NodeInfo{
		NodeID:     privKey.Public().NodeID(),
		ListenAddr: endpoint.String(),
		Moniker:    string(privKey.Public().NodeID()),
		Network:    "test",
	}
	router, err := NewRouter(
		privKey,
		func() *types.NodeInfo { return &nodeInfo },
		dbm.NewMemDB(),
		&RouterOptions{
			Endpoint:                 endpoint,
			Connection:               conn.DefaultMConnConfig(),
			IncomingConnectionWindow: utils.Some[time.Duration](0),
			MaxAcceptRate:            rate.Inf,
			MaxDialRate:              rate.Every(10 * time.Second),
			// Short, so the test fails fast rather than waiting out the 10s default.
			HandshakeTimeout: utils.Some(100 * time.Millisecond),
		},
	)
	require.NoError(t, err)
	require.NoError(t, router.Start(ctx))
	require.NoError(t, router.WaitForStart(ctx))
	t.Cleanup(router.Stop)

	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		tcpConn, err := tcp.Dial(ctx, endpoint.AddrPort)
		if err != nil {
			return err
		}
		s.SpawnBg(func() error { return tcpConn.Run(ctx) })

		// Complete the handshake, which authenticates us, then send no node info.
		if _, err := handshake(ctx, tcpConn, NodeSecretKey(ed25519.GenerateSecretKey()), handshakeSpec{}); err != nil {
			return err
		}

		// Keep a read outstanding so the connection pump observes the close. Without
		// the deadline covering exchangeNodeInfo the router never hangs up and this
		// never returns; the go test timeout is the backstop.
		buf := make([]byte, 1)
		for ctx.Err() == nil {
			if err := tcpConn.Read(ctx, buf); err != nil {
				break
			}
		}
		return nil
	})
	// The router hanging up on us surfaces as EOF from the connection pump.
	require.ErrorIs(t, err, io.EOF)
}
