package p2p

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"

	"fmt"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
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
		Version: "1.2.3",
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
	h := spawnRouterWithOptions(t, logger, opts)

	err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var total atomic.Int64
		t.Logf("spawn a bunch of connections, making sure that no more than %d are accepted at any given time", opts.MaxAcceptedConnections)
		for range 10 {
			s.Spawn(func() error {
				key, info := makeKeyAndInfo()
				// Establish a connection.
				tcpConn, err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
				if err != nil {
					return fmt.Errorf("tcp.Dial(): %w", err)
				}
				conn, err := handshake(ctx, logger, tcpConn, info, key)
				if err != nil {
					return fmt.Errorf("handshake(): %w", err)
				}
				defer conn.Close()
				// Check that limit was not exceeded.
				if got, wantMax := total.Add(1), int64(opts.MaxAcceptedConnections); got > wantMax {
					return fmt.Errorf("accepted too many connections: %d > %d", got, wantMax)
				}
				defer total.Add(-1)
				// Keep the connection open for a while to force other dialers to wait.
				if err := utils.Sleep(ctx, 100*time.Millisecond); err != nil {
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
	testcases := []netip.Addr{
		netip.IPv4Unspecified(),
		tcp.IPv4Loopback(),
		netip.IPv6Unspecified(),
		netip.IPv6Loopback(),
	}

	for _, tc := range testcases {
		t.Run(tc.String(), func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			ctx := t.Context()
			t.Cleanup(leaktest.Check(t))
			opts := makeRouterOptions()
			opts.Endpoint.AddrPort = netip.AddrPortFrom(tc, opts.Endpoint.Port())
			h := spawnRouterWithOptions(t, logger, opts)
			if got, want := h.router.Endpoint().Addr(), tc; got != want {
				t.Fatalf("transport.Endpoint() = %v, want %v", got, want)
			}
			tcpConn, err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
			if err != nil {
				t.Fatalf("tcp.Dial(): %v", err)
			}
			defer tcpConn.Close()
			key, info := makeKeyAndInfo()
			if _, err := handshake(ctx, logger, tcpConn, info, key); err != nil {
				t.Fatalf("handshake(): %v", err)
			}
		})
	}
}

// Test checking that handshake provides correct NodeInfo.
func TestHandshake_NodeInfo(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	h := spawnRouter(t, logger)
	tcpConn, err := tcp.Dial(ctx, h.router.Endpoint().AddrPort)
	if err != nil {
		t.Fatalf("tcp.Dial(): %v", err)
	}
	defer tcpConn.Close()
	key, info := makeKeyAndInfo()
	conn, err := handshake(ctx, logger, tcpConn, info, key)
	if err != nil {
		t.Fatalf("handshake(): %v", err)
	}
	defer conn.Close()
	if err := utils.TestDiff(selfInfo, conn.PeerInfo()); err != nil {
		t.Fatalf("conn.PeerInfo(): %v", err)
	}
}

// Test checking that handshake respects the context.
func TestHandshake_Context(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	ctx := t.Context()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		addr := tcp.TestReserveAddr()
		listener, err := tcp.Listen(addr)
		if err != nil {
			return fmt.Errorf("tcp.Listen(): %w", err)
		}
		defer listener.Close()

		s.SpawnBg(func() error {
			// One connection end does not handshake.
			tcpConn, err := tcp.AcceptOrClose(ctx, listener)
			if err != nil {
				return fmt.Errorf("tcp.AcceptOrClose(): %w", err)
			}
			defer tcpConn.Close()
			<-ctx.Done()
			return nil
		})

		tcpConn, err := tcp.Dial(ctx, addr)
		if err != nil {
			t.Fatalf("tcp.Dial(): %v", err)
		}

		s.SpawnBg(func() error {
			defer tcpConn.Close()
			// Second connection end tries to handshake.
			key, info := makeKeyAndInfo()
			conn, err := handshake(ctx, logger, tcpConn, info, key)
			if err == nil {
				defer conn.Close()
				return fmt.Errorf("handshake(): expected error, got %w", err)
			}
			return nil
		})
		// Cancel context, which should make both connection terminate gracefully.
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRouter_SendReceive_Random(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	network := MakeTestNetwork(t, TestNetworkOptions{NumNodes: 5})
	channels := map[ChannelID]map[types.NodeID]*Channel{}
	for id := range ChannelID(4) {
		channels[id] = network.MakeChannels(t, makeChDesc(id))
	}
	nodes := network.NodeIDs()
	network.Start(t)
	for i := range 100 {
		t.Logf("ITER %v", i)
		from := nodes[rng.Intn(len(nodes))]
		to := nodes[rng.Intn(len(nodes))]
		if from == to {
			continue
		}
		chID := ChannelID(rng.Intn(len(channels)))
		want := &TestMessage{Value: utils.GenString(rng, 10)}

		if err := channels[chID][from].Send(ctx, Envelope{
			ChannelID: chID,
			Message:   want,
			To:        to,
		}); err != nil {
			t.Fatalf("Send(): %v", err)
		}
		got, err := channels[chID][to].Receive1(ctx)
		if err != nil {
			t.Fatalf("Receive1(): %v", err)
		}
		if err := utils.TestDiff[proto.Message](want, got.Message); err != nil {
			t.Fatalf("Receive1(): %v", err)
		}
	}
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
