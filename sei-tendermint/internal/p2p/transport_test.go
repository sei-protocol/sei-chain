package p2p

import (
	"context"
	"net/netip"
	"testing"
	"sync/atomic"
	"time"

	"github.com/fortytw2/leaktest"

	"fmt"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
)

func makeKeyAndInfo() (crypto.PrivKey, types.NodeInfo) {
	peerKey := ed25519.GenPrivKey()
	nodeID := types.NodeIDFromPubKey(peerKey.PubKey())
	peerInfo := types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: "127.0.0.1:1239",
		Network:    "test",
		Moniker:    string(nodeID),
		Channels:   []byte{0x01, 0x02},
		ProtocolVersion: types.ProtocolVersion{
			P2P:   1,
			Block: 2,
			App:   3,
		},
		Version:    "1.2.3",
		Other: types.NodeInfoOther{
			TxIndex:    "on",
			RPCAddress: "rpc.domain.com",
		},
	}
	return peerKey, peerInfo
}

func TestRouter_MaxAcceptedConnections(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	opts := makeRouterOptions()
	opts.MaxAcceptedConnections = 2
	h := spawnRouterWithOptions(t,logger,opts)

	err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var total atomic.Int64
		t.Logf("spawn a bunch of connections, making sure that no more than %d are accepted at any given time", opts.MaxAcceptedConnections)
		for range 10 {
			s.Spawn(func() error {
				key, info := makeKeyAndInfo()
				// Establish a connection.
				tcpConn,err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
				if err!=nil {
					return fmt.Errorf("tcp.Dial(): %w", err)
				}
				conn,err := handshake(ctx, logger, tcpConn, info, key)
				if err!=nil {
					return fmt.Errorf("handshake(): %w", err)
				}
				defer conn.Close()
				// Check that limit was not exceeded.
				if got,wantMax:=total.Add(1),int64(opts.MaxAcceptedConnections); got>wantMax {
					return fmt.Errorf("accepted too many connections: %d > %d", got, wantMax)
				}
				defer total.Add(-1)
				// Keep the connection open for a while to force other dialers to wait.
				if err:=utils.Sleep(ctx, 100 * time.Millisecond); err!=nil {
					return err
				}
				return nil
			})
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
}

// Test checking if listening on various local interfaces works.
func TestRouter_Listen(t *testing.T) {
	reservePort := func(ip netip.Addr) Endpoint {
		addr := tcp.TestReserveAddr()
		return Endpoint{netip.AddrPortFrom(ip, addr.Port())}
	}

	testcases := []Endpoint{
		reservePort(netip.IPv4Unspecified()),
		reservePort(tcp.IPv4Loopback()),
		reservePort(netip.IPv6Unspecified()),
		reservePort(netip.IPv6Loopback()),
	}

	for _, tc := range testcases {
		t.Run(tc.String(), func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			ctx := t.Context()
			t.Cleanup(leaktest.Check(t))
			opts := makeRouterOptions()
			opts.Endpoint = tc
			h := spawnRouterWithOptions(t, logger, opts)
			if got, want := h.router.Endpoint(), tc; got != want {
				t.Fatalf("transport.Endpoint() = %v, want %v", got, want)
			}
			tcpConn,err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
			if err!=nil {
				t.Fatalf("tcp.Dial(): %w", err)
			}
			defer tcpConn.Close()
			key, info := makeKeyAndInfo()
			if _, err := handshake(ctx, logger, tcpConn, info, key); err != nil {
				t.Fatalf("handshake(): %w", err)
			}
		})
	}
}

// Test checking that handshake provides correct NodeInfo.
func TestConnection_Handshake(t *testing.T) {
	ctx := t.Context()
	// TODO
}

// Test checking that handshake respects the context.
func TestConnection_HandshakeCancel(t *testing.T) {
	ctx := t.Context()
	// TODO
}

func TestConnection_SendReceive(t *testing.T) {
	ctx := t.Context()
	a := makeRouter(ctx)
	b := makeRouter(ctx)
	ab, ba := dialAcceptHandshake(ctx, t, a, b)

	// Can send and receive a to b.
	err := ab.SendMessage(ctx, chID, []byte("foo"))
	require.NoError(t, err)

	ch, msg, err := ba.ReceiveMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("foo"), msg)
	require.Equal(t, chID, ch)

	// Can send and receive b to a.
	err = ba.SendMessage(ctx, chID, []byte("bar"))
	require.NoError(t, err)

	_, msg, err = ab.ReceiveMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("bar"), msg)

	// Close one side of the connection. Both sides should then error
	// with io.EOF when trying to send or receive.
	ba.Close()

	_, _, err = ab.ReceiveMessage(ctx)
	require.Error(t, err)

	err = ab.SendMessage(ctx, chID, []byte("closed"))
	require.Error(t, err)

	_, _, err = ba.ReceiveMessage(ctx)
	require.Error(t, err)

	err = ba.SendMessage(ctx, chID, []byte("closed"))
	require.Error(t, err)
}

func TestEndpoint_NodeAddress(t *testing.T) {
	var (
		ip4 = netip.AddrFrom4([4]byte{1, 2, 3, 4})
		ip6 = netip.AddrFrom16([16]byte{0xb1, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01})
		id  = types.NodeID("00112233445566778899aabbccddeeff00112233")
	)

	testcases := []struct {
		endpoint Endpoint
		expect   NodeAddress
	}{
		// Valid endpoints.
		{
			Endpoint{netip.AddrPortFrom(ip4, 8080)},
			NodeAddress{Hostname: "1.2.3.4", Port: 8080},
		},
		{
			Endpoint{netip.AddrPortFrom(ip6, 8080)},
			NodeAddress{Hostname: "b10c::1", Port: 8080},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.endpoint.String(), func(t *testing.T) {
			// Without NodeID.
			expect := tc.expect
			require.Equal(t, expect, tc.endpoint.NodeAddress(""))

			// With NodeID.
			expect.NodeID = id
			require.Equal(t, expect, tc.endpoint.NodeAddress(expect.NodeID))
		})
	}
}

func TestEndpoint_Validate(t *testing.T) {
	ip4 := netip.AddrFrom4([4]byte{1, 2, 3, 4})
	ip6 := netip.AddrFrom16([16]byte{0xb1, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01})

	testcases := []struct {
		endpoint    Endpoint
		expectValid bool
	}{
		// Valid endpoints.
		{Endpoint{netip.AddrPortFrom(ip4, 0)}, true},
		{Endpoint{netip.AddrPortFrom(ip6, 0)}, true},
		{Endpoint{netip.AddrPortFrom(ip4, 8008)}, true},

		// Invalid endpoints.
		{Endpoint{}, false},
	}
	for _, tc := range testcases {
		t.Run(tc.endpoint.String(), func(t *testing.T) {
			err := tc.endpoint.Validate()
			if tc.expectValid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
