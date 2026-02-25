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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
)

func makeInfo(key NodeSecretKey) types.NodeInfo {
	nodeID := key.Public().NodeID()
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

func TestRouter_MaxConcurrentAccepts(t *testing.T) {
	logger, _ := log.NewDefaultLogger("plain", "debug")
	rng := utils.TestRng()
	opts := makeRouterOptions()
	maxAccepts := 2
	opts.MaxConcurrentAccepts = utils.Some(maxAccepts)

	err := utils.IgnoreCancel(scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouterWithOptions(logger, rng, opts)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}

		var total atomic.Int64
		t.Logf("spawn a bunch of connections, making sure that no more than %d are accepted at any given time", maxAccepts)
		for range 10 {
			s.SpawnNamed("test", func() error {
				return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
					x := makeRouter(logger, rng)
					// Establish a connection.
					addr := TestAddress(r)
					tcpConn, err := x.dial(ctx, addr)
					if err != nil {
						return fmt.Errorf("tcp.dial(): %w", err)
					}
					s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, tcpConn.Run(ctx)) })
					// Begin handshake (but not finish)
					var input [1]byte
					if err := tcpConn.Read(ctx, input[:]); err != nil {
						return fmt.Errorf("tcpConn.Read(): %w", err)
					}
					// Check that limit was not exceeded.
					if got, wantMax := total.Add(1), int64(maxAccepts); got > wantMax {
						return fmt.Errorf("accepted too many connections: %d > %d", got, wantMax)
					}
					defer total.Add(-1)
					// Keep the connection open for a while to force other dialers to wait.
					if err := utils.Sleep(ctx, 100*time.Millisecond); err != nil {
						return err
					}
					return nil
				})
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
			rng := utils.TestRng()
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				opts := makeRouterOptions()
				opts.Endpoint.AddrPort = tc
				r := makeRouterWithOptions(logger, rng, opts)
				s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
				if err := r.WaitForStart(ctx); err != nil {
					return err
				}

				if got, want := r.Endpoint().AddrPort, tc; got != want {
					return fmt.Errorf("r.Endpoint() = %v, want %v", got, want)
				}

				x := makeRouter(logger, rng)
				addr := TestAddress(r)
				tcpConn, err := x.dial(ctx, addr)
				if err != nil {
					return fmt.Errorf("tcp.dial(): %v", err)
				}
				s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, tcpConn.Run(ctx)) })
				if _, _, err := x.handshakeV2(ctx, tcpConn, utils.Some(addr)); err != nil {
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
	rng := utils.TestRng()
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		r := makeRouter(logger, rng)
		s.SpawnBg(func() error { return utils.IgnoreCancel(r.Run(ctx)) })
		if err := r.WaitForStart(ctx); err != nil {
			return err
		}

		x := makeRouter(logger, rng)
		addr := TestAddress(r)
		tcpConn, err := x.dial(ctx, addr)
		if err != nil {
			return fmt.Errorf("tcp.dial(): %v", err)
		}
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, tcpConn.Run(ctx)) })
		_, info, err := x.handshakeV2(ctx, tcpConn, utils.Some(addr))
		if err != nil {
			return fmt.Errorf("handshake(): %v", err)
		}
		if err := utils.TestDiff(*r.nodeInfoProducer(), info); err != nil {
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
	rng := utils.TestRng()
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		a := makeRouter(logger, rng)
		b := makeRouter(logger, rng)
		listener, err := tcp.Listen(a.Endpoint().AddrPort)
		if err != nil {
			return fmt.Errorf("tcp.Listen(): %w", err)
		}
		defer listener.Close()
		s.Spawn(func() error {
			// One connection end tries to handshake.
			addr := TestAddress(a)
			tcpConn, err := b.dial(ctx, addr)
			if err != nil {
				return fmt.Errorf("tcp.dial(): %v", err)
			}
			s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, tcpConn.Run(ctx)) })
			s.SpawnBg(func() error {
				if _, _, err := b.handshakeV2(ctx, tcpConn, utils.Some(addr)); err == nil {
					return fmt.Errorf("handshake(): expected error, got %w", err)
				}
				return nil
			})
			return nil
		})
		// Second connection end does not handshake.
		tcpConn, err := listener.AcceptOrClose(ctx)
		if err != nil {
			return fmt.Errorf("tcp.AcceptOrClose(): %w", err)
		}
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, tcpConn.Run(ctx)) })
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
	channels := map[ChannelID]map[types.NodeID]*Channel[*TestMessage]{}
	for id := range ChannelID(4) {
		channels[id] = TestMakeChannels(t, network, makeChDesc(id))
	}
	nodes := network.NodeIDs()
	network.Start(t)
	for range 100 {
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
