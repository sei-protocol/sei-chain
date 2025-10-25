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

func makeKey() crypto.PrivKey {
	return ed25519.GenPrivKey()
}

func makeInfo(key crypto.PrivKey) types.NodeInfo {
	nodeID := types.NodeIDFromPubKey(key.PubKey())
	peerInfo := types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: "127.0.0.1:1239",
		Network:    "test",
		Moniker:    string(nodeID),
		Channels:   []byte{},
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
	return peerInfo
}

func TestRouter_MaxAcceptedConnections(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	opts := makeRouterOptions()
	opts.MaxAcceptedConnections = 2

	err := utils.IgnoreCancel(scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouterWithOptions(logger, opts)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}

		var total atomic.Int64
		t.Logf("spawn a bunch of connections, making sure that no more than %d are accepted at any given time", opts.MaxAcceptedConnections)
		for range 10 {
			s.SpawnNamed("test", func() error {
				x := makeRouter(logger)
				// Establish a connection.
				tcpConn, err := x.Dial(ctx, TestAddress(r))
				if err != nil {
					return fmt.Errorf("tcp.Dial(): %w", err)
				}
				conn, err := HandshakeOrClose(ctx, x, tcpConn)
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
	testcases := []netip.AddrPort{
		tcp.TestReservePort(netip.IPv4Unspecified()),
		tcp.TestReservePort(tcp.IPv4Loopback()),
		tcp.TestReservePort(netip.IPv6Unspecified()),
		tcp.TestReservePort(netip.IPv6Loopback()),
	}

	for _, tc := range testcases {
		t.Run(tc.Addr().String(), func(t *testing.T) {
			logger, _ := log.NewDefaultLogger("plain", "debug")
			t.Cleanup(leaktest.Check(t))
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				opts := makeRouterOptions()
				opts.Endpoint.AddrPort = tc
				r := makeRouterWithOptions(logger, opts)
				s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
				if err := r.WaitForStart(ctx); err != nil {
					return err
				}

				if got, want := r.Endpoint().AddrPort, tc; got != want {
					return fmt.Errorf("r.Endpoint() = %v, want %v", got, want)
				}

				x := makeRouter(logger)
				tcpConn, err := x.Dial(ctx, TestAddress(r))
				if err != nil {
					return fmt.Errorf("tcp.Dial(): %v", err)
				}
				defer tcpConn.Close()
				if _, err := HandshakeOrClose(ctx, x, tcpConn); err != nil {
					return fmt.Errorf("handshake(): %v", err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Test checking that handshake provides correct NodeInfo.
func TestHandshake_NodeInfo(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouter(logger)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}

		x := makeRouter(logger)
		tcpConn, err := x.Dial(ctx, TestAddress(r))
		if err != nil {
			return fmt.Errorf("tcp.Dial(): %v", err)
		}
		defer tcpConn.Close()
		conn, err := HandshakeOrClose(ctx, x, tcpConn)
		if err != nil {
			return fmt.Errorf("handshake(): %v", err)
		}
		defer conn.Close()
		if err := utils.TestDiff(*r.nodeInfoProducer(), conn.PeerInfo()); err != nil {
			t.Fatalf("conn.PeerInfo(): %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Test checking that handshake respects the context.
func TestHandshake_Context(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		a := makeRouter(logger)
		b := makeRouter(logger)
		listener, err := tcp.Listen(a.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("tcp.Listen(): %w", err)
		}
		s.Spawn(func() error {
			defer listener.Close()
			// One connection end does not handshake.
			tcpConn, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return fmt.Errorf("tcp.AcceptOrClose(): %w", err)
			}
			s.SpawnBg(func() error {
				defer tcpConn.Close()
				<-ctx.Done()
				return nil
			})
			return nil
		})
		s.Spawn(func() error {
			// Second connection end tries to handshake.
			tcpConn, err := b.Dial(ctx, TestAddress(a))
			if err != nil {
				t.Fatalf("tcp.Dial(): %v", err)
			}
			s.SpawnBg(func() error {
				defer tcpConn.Close()
				conn, err := HandshakeOrClose(ctx, b, tcpConn)
				if err == nil {
					defer conn.Close()
					return fmt.Errorf("handshake(): expected error, got %w", err)
				}
				return nil
			})
			return nil
		})
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

		channels[chID][from].Send(want, to)
		got, err := channels[chID][to].Recv(ctx)
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
