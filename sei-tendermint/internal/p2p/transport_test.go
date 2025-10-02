package p2p_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"errors"
	"fmt"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/types"
	"io"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
)

func makeKeyAndInfo() (crypto.PrivKey, types.NodeInfo) {
	peerKey := ed25519.GenPrivKey()
	nodeID := types.NodeIDFromPubKey(peerKey.PubKey())
	peerInfo := types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: "0.0.0.0:0",
		Network:    "test",
		Moniker:    string(nodeID),
		Channels:   []byte{0x01, 0x02},
	}
	return peerKey, peerInfo
}

// Establishes a connection to the transport.
// Returns both ends of the connection.
func connect(ctx context.Context, tr *p2p.Transport) (c1 *p2p.Connection, c2 *p2p.Connection, err error) {
	defer func() {
		if err != nil {
			if c1 != nil {
				c1.Close()
			}
			if c2 != nil {
				c2.Close()
			}
		}
	}()
	// Here we are utilizing the fact that Transport accepts connection proactively
	// before Accept is called.
	c1, err = tr.Dial(ctx, tr.Endpoint())
	if err != nil {
		return nil, nil, fmt.Errorf("Dial(): %w", err)
	}
	c2, err = tr.Accept(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Accept(): %w", err)
	}
	if got, want := c1.LocalEndpoint(), c2.RemoteEndpoint(); got != want {
		return nil, nil, fmt.Errorf("c1.LocalEndpoint() = %v, want %v", got, want)
	}
	if got, want := c1.RemoteEndpoint(), c2.LocalEndpoint(); got != want {
		return nil, nil, fmt.Errorf("c1.RemoteEndpoint() = %v, want %v", got, want)
	}
	return c1, c2, nil
}

func TestTransport_AcceptMaxAcceptedConnections(t *testing.T) {
	ctx := t.Context()
	transport := p2p.NewTransport(
		log.NewNopLogger(),
		p2p.Endpoint{tcp.TestReserveAddr()},
		conn.DefaultMConnConfig(),
		[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
		p2p.TransportOptions{
			MaxAcceptedConnections: 2,
		},
	)

	err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("transport", func() error { return transport.Run(ctx) })
		if err := transport.WaitForStart(ctx); err != nil {
			return err
		}
		t.Logf("The first two connections should be accepted just fine.")

		a1, a2, err := connect(ctx, transport)
		if err != nil {
			return fmt.Errorf("1st connect(): %w", err)
		}
		defer a1.Close()
		defer a2.Close()

		b1, b2, err := connect(ctx, transport)
		if err != nil {
			return fmt.Errorf("2nd connect(): %w", err)
		}
		defer b1.Close()
		defer b2.Close()

		t.Logf("The third connection will be dialed successfully, but the accept should not go through.")
		c1, err := transport.Dial(ctx, transport.Endpoint())
		if err != nil {
			return fmt.Errorf("3rd Dial(): %w", err)
		}
		defer c1.Close()
		if err := utils.WithTimeout(ctx, time.Second, func(ctx context.Context) error {
			c2, err := transport.Accept(ctx)
			if err == nil {
				c2.Close()
			}
			return err
		}); !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("Accept() over cap: %v, want %v", err, context.DeadlineExceeded)
		}

		t.Logf("once either of the other connections are closed, the accept goes through.")
		a1.Close()
		a2.Close() // we close both a1 and a2 to make sure the connection count drops below the limit.
		c2, err := transport.Accept(ctx)
		if err != nil {
			return fmt.Errorf("3rd Accept(): %w", err)
		}
		defer c2.Close()
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransport_Listen(t *testing.T) {
	reservePort := func(ip netip.Addr) netip.AddrPort {
		addr := tcp.TestReserveAddr()
		return netip.AddrPortFrom(ip, addr.Port())
	}

	testcases := []struct {
		endpoint p2p.Endpoint
		ok       bool
	}{
		// Valid v4 and v6 addresses, with mconn and tcp protocols.
		{p2p.Endpoint{reservePort(netip.IPv4Unspecified())}, true},
		{p2p.Endpoint{reservePort(tcp.IPv4Loopback())}, true},
		{p2p.Endpoint{reservePort(netip.IPv6Unspecified())}, true},
		{p2p.Endpoint{reservePort(netip.IPv6Loopback())}, true},

		// Invalid endpoints.
		{p2p.Endpoint{}, false},
	}

	aKey, aInfo := makeKeyAndInfo()
	bKey, bInfo := makeKeyAndInfo()
	for _, tc := range testcases {
		t.Run(tc.endpoint.String(), func(t *testing.T) {
			ctx := t.Context()
			t.Cleanup(leaktest.Check(t))

			transport := p2p.NewTransport(
				log.NewNopLogger(),
				tc.endpoint,
				conn.DefaultMConnConfig(),
				[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
				p2p.TransportOptions{},
			)
			if got, want := transport.Endpoint(), tc.endpoint; got != want {
				t.Fatalf("transport.Endpoint() = %v, want %v", got, want)
			}

			err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.SpawnBgNamed("transport", func() error { return transport.Run(ctx) })
				if err := transport.WaitForStart(ctx); err != nil {
					return err
				}
				s.SpawnNamed("dial", func() error {
					conn, err := transport.Dial(ctx, tc.endpoint)
					if err != nil {
						return fmt.Errorf("transport.Dial(): %w", err)
					}
					defer conn.Close()
					if _, err := conn.Handshake(ctx, aInfo, aKey); err != nil {
						return fmt.Errorf("conn.Handshake(): %w", err)
					}
					if err := conn.Close(); err != nil {
						return fmt.Errorf("conn.Close(): %w", err)
					}
					if _, _, err := conn.ReceiveMessage(ctx); !errors.Is(err, io.EOF) {
						return fmt.Errorf("conn.ReceiveMessage() =  %v, want %v", err, io.EOF)
					}
					return nil
				})
				s.SpawnNamed("accept", func() error {
					conn, err := transport.Accept(ctx)
					if err != nil {
						return fmt.Errorf("transport.Accept(): %w", err)
					}
					defer conn.Close()
					if _, err := conn.Handshake(ctx, bInfo, bKey); err != nil {
						return fmt.Errorf("conn.Handshake(): %w", err)
					}
					if err := conn.Close(); err != nil {
						return fmt.Errorf("conn.Close(): %w", err)
					}
					if _, _, err := conn.ReceiveMessage(ctx); !errors.Is(err, io.EOF) {
						return fmt.Errorf("conn.ReceiveMessage() =  %v, want %v", err, io.EOF)
					}
					return nil
				})
				return nil
			}))
			if !tc.ok {
				var want p2p.InvalidEndpointErr
				if !errors.As(err, &want) {
					t.Fatalf("error = %v, want %T", err, want)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			// Dialing the closed endpoint should error
			_, err = transport.Dial(ctx, tc.endpoint)
			require.Error(t, err)
		})
	}
}

// transportFactory is used to set up transports for tests.
type transportFactory = func(ctx context.Context) *p2p.Transport

// testTransports is a registry of transport factories for withTransports().
var testTransports = map[string](func() transportFactory){}

// withTransports is a test helper that runs a test against all transports
// registered in testTransports.
func withTransports(t *testing.T, tester func(*testing.T, transportFactory)) {
	t.Helper()
	t.Cleanup(leaktest.Check(t))
	tester(t, func(ctx context.Context) *p2p.Transport {
		logger, _ := log.NewDefaultLogger("plain", "info")
		transport := p2p.NewTransport(
			logger,
			p2p.Endpoint{tcp.TestReserveAddr()},
			conn.DefaultMConnConfig(),
			[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
			p2p.TransportOptions{},
		)
		go func() {
			if err := transport.Run(ctx); err != nil {
				panic(err)
			}
		}()
		if err := transport.WaitForStart(ctx); err != nil {
			panic(err)
		}
		return transport
	})
}

func TestTransport_DialEndpoints(t *testing.T) {
	ipTestCases := []struct {
		ip netip.Addr
		ok bool
	}{
		{netip.IPv4Unspecified(), true},
		{netip.IPv6Unspecified(), true},

		{netip.AddrFrom4([4]byte{255, 255, 255, 255}), false},
		{netip.AddrFrom4([4]byte{224, 0, 0, 1}), false},
	}

	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		endpoint := a.Endpoint()

		// Spawn a goroutine to simply accept any connections until closed.
		go func() {
			for {
				conn, err := a.Accept(ctx)
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()

		// Dialing self should work.
		conn, err := a.Dial(ctx, endpoint)
		require.NoError(t, err)
		require.NoError(t, conn.Close())

		// Dialing empty endpoint should error.
		_, err = a.Dial(ctx, p2p.Endpoint{})
		require.Error(t, err)

		// Tests for networked endpoints (with IP).
		for _, tc := range ipTestCases {
			t.Run(tc.ip.String(), func(t *testing.T) {
				e := endpoint
				e.AddrPort = netip.AddrPortFrom(tc.ip, endpoint.Port())
				conn, err := a.Dial(ctx, e)
				if tc.ok {
					require.NoError(t, err)
					require.NoError(t, conn.Close())
				} else {
					require.Error(t, err, "endpoint=%s", e)
				}
			})
		}
	})
}

func TestTransport_Endpoints(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)

		// Both transports return valid and different endpoints.
		aEndpoint := a.Endpoint()
		bEndpoint := b.Endpoint()
		require.NotEqual(t, aEndpoint, bEndpoint)
		for _, endpoint := range []p2p.Endpoint{aEndpoint, bEndpoint} {
			err := endpoint.Validate()
			require.NoError(t, err, "invalid endpoint %q", endpoint)
		}
	})
}

func TestTransport_String(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		a := makeTransport(t.Context())
		require.NotEmpty(t, a.String())
	})
}

func TestConnection_Handshake(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)
		ab, ba := dialAccept(ctx, t, a, b)

		// A handshake should pass the given keys and NodeInfo.
		aKey := ed25519.GenPrivKey()
		aInfo := types.NodeInfo{
			NodeID: types.NodeIDFromPubKey(aKey.PubKey()),
			ProtocolVersion: types.ProtocolVersion{
				P2P:   1,
				Block: 2,
				App:   3,
			},
			ListenAddr: "127.0.0.1:1239",
			Network:    "network",
			Version:    "1.2.3",
			Channels:   bytes.HexBytes([]byte{0xf0, 0x0f}),
			Moniker:    "moniker",
			Other: types.NodeInfoOther{
				TxIndex:    "on",
				RPCAddress: "rpc.domain.com",
			},
		}
		bKey := ed25519.GenPrivKey()
		bInfo := types.NodeInfo{
			NodeID:     types.NodeIDFromPubKey(bKey.PubKey()),
			ListenAddr: "127.0.0.1:1234",
			Moniker:    "othermoniker",
			Other: types.NodeInfoOther{
				TxIndex: "off",
			},
		}

		errCh := make(chan error, 1)
		go func() {
			// Must use assert due to goroutine.
			peerInfo, err := ba.Handshake(ctx, bInfo, bKey)
			if err == nil {
				assert.Equal(t, aInfo, peerInfo)
			}
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}()

		peerInfo, err := ab.Handshake(ctx, aInfo, aKey)
		require.NoError(t, err)
		require.Equal(t, bInfo, peerInfo)

		require.NoError(t, <-errCh)
	})
}

func TestConnection_HandshakeCancel(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)

		// Handshake should error on context cancellation.
		ab, ba := dialAccept(ctx, t, a, b)
		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
		cancel()
		_, err := ab.Handshake(timeoutCtx, types.NodeInfo{}, ed25519.GenPrivKey())
		require.Error(t, err)
		_ = ab.Close()
		_ = ba.Close()

		// Handshake should error on context timeout.
		ab, ba = dialAccept(ctx, t, a, b)
		timeoutCtx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_, err = ab.Handshake(timeoutCtx, types.NodeInfo{}, ed25519.GenPrivKey())
		require.Error(t, err)
		_ = ab.Close()
		_ = ba.Close()
	})
}

func TestConnection_FlushClose(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)
		ab, _ := dialAcceptHandshake(ctx, t, a, b)

		ab.Close()

		_, _, err := ab.ReceiveMessage(ctx)
		require.Error(t, err)

		err = ab.SendMessage(ctx, chID, []byte("closed"))
		require.Error(t, err)
	})
}

func TestConnection_LocalRemoteEndpoint(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)
		ab, ba := dialAcceptHandshake(ctx, t, a, b)

		// Local and remote connection endpoints correspond to each other.
		require.NotEmpty(t, ab.LocalEndpoint())
		require.NotEmpty(t, ba.LocalEndpoint())
		require.Equal(t, ab.LocalEndpoint(), ba.RemoteEndpoint())
		require.Equal(t, ab.RemoteEndpoint(), ba.LocalEndpoint())
	})
}

func TestConnection_SendReceive(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)
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
	})
}

func TestConnection_String(t *testing.T) {
	withTransports(t, func(t *testing.T, makeTransport transportFactory) {
		ctx := t.Context()
		a := makeTransport(ctx)
		b := makeTransport(ctx)
		ab, _ := dialAccept(ctx, t, a, b)
		require.NotEmpty(t, ab.String())
	})
}

func TestEndpoint_NodeAddress(t *testing.T) {
	var (
		ip4 = netip.AddrFrom4([4]byte{1, 2, 3, 4})
		ip6 = netip.AddrFrom16([16]byte{0xb1, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01})
		id  = types.NodeID("00112233445566778899aabbccddeeff00112233")
	)

	testcases := []struct {
		endpoint p2p.Endpoint
		expect   p2p.NodeAddress
	}{
		// Valid endpoints.
		{
			p2p.Endpoint{netip.AddrPortFrom(ip4, 8080)},
			p2p.NodeAddress{Hostname: "1.2.3.4", Port: 8080},
		},
		{
			p2p.Endpoint{netip.AddrPortFrom(ip6, 8080)},
			p2p.NodeAddress{Hostname: "b10c::1", Port: 8080},
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
		endpoint    p2p.Endpoint
		expectValid bool
	}{
		// Valid endpoints.
		{p2p.Endpoint{netip.AddrPortFrom(ip4, 0)}, true},
		{p2p.Endpoint{netip.AddrPortFrom(ip6, 0)}, true},
		{p2p.Endpoint{netip.AddrPortFrom(ip4, 8008)}, true},

		// Invalid endpoints.
		{p2p.Endpoint{}, false},
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

// dialAccept is a helper that dials b from a and returns both sides of the
// connection.
func dialAccept(ctx context.Context, t *testing.T, a, b *p2p.Transport) (*p2p.Connection, *p2p.Connection) {
	t.Helper()

	endpoint := b.Endpoint()

	var acceptConn *p2p.Connection
	var dialConn *p2p.Connection
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var err error
			if dialConn, err = a.Dial(ctx, endpoint); err != nil {
				return err
			}
			t.Cleanup(func() { _ = dialConn.Close() })
			return nil
		})
		var err error
		if acceptConn, err = b.Accept(ctx); err != nil {
			return err
		}
		t.Cleanup(func() { _ = acceptConn.Close() })
		return nil
	}); err != nil {
		t.Fatalf("dial/accept failed: %v", err)
	}
	return dialConn, acceptConn
}

// dialAcceptHandshake is a helper that dials and handshakes b from a and
// returns both sides of the connection.
func dialAcceptHandshake(ctx context.Context, t *testing.T, a, b *p2p.Transport) (*p2p.Connection, *p2p.Connection) {
	t.Helper()

	ab, ba := dialAccept(ctx, t, a, b)

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			privKey := ed25519.GenPrivKey()
			nodeInfo := types.NodeInfo{
				NodeID:     types.NodeIDFromPubKey(privKey.PubKey()),
				ListenAddr: "127.0.0.1:1235",
				Moniker:    "a",
			}
			_, err := ba.Handshake(ctx, nodeInfo, privKey)
			return err
		})
		privKey := ed25519.GenPrivKey()
		nodeInfo := types.NodeInfo{
			NodeID:     types.NodeIDFromPubKey(privKey.PubKey()),
			ListenAddr: "127.0.0.1:1234",
			Moniker:    "b",
		}
		_, err := ab.Handshake(ctx, nodeInfo, privKey)
		return err
	})

	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	return ab, ba
}
