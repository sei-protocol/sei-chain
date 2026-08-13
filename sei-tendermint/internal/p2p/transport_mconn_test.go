package p2p_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"

	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
)

// Transports are mainly tested by common tests in transport_test.go, we
// register a transport factory here to get included in those tests.
func init() {
	testTransports["mconn"] = func() func(context.Context) p2p.Transport {
		return func(ctx context.Context) p2p.Transport {
			transport := p2p.NewMConnTransport(
				log.NewNopLogger(),
				p2p.Endpoint{
					Protocol: p2p.MConnProtocol,
					Addr:     tcp.TestReserveAddr(),
				},
				conn.DefaultMConnConfig(),
				[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
				p2p.MConnTransportOptions{},
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
		}
	}
}

// Establishes a connection to the transport.
// Returns both ends of the connection.
func connect(ctx context.Context, tr *p2p.MConnTransport) (c1 p2p.Connection, c2 p2p.Connection, err error) {
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
	// Here we are utilizing the fact that MConnTransport accepts connection proactively
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

func TestMConnTransport_AcceptMaxAcceptedConnections(t *testing.T) {
	ctx := t.Context()
	transport := p2p.NewMConnTransport(
		log.NewNopLogger(),
		p2p.Endpoint{
			Protocol: p2p.MConnProtocol,
			Addr:     tcp.TestReserveAddr(),
		},
		conn.DefaultMConnConfig(),
		[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
		p2p.MConnTransportOptions{
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

func TestMConnTransport_Listen(t *testing.T) {
	reservePort := func(ip netip.Addr) netip.AddrPort {
		addr := tcp.TestReserveAddr()
		return netip.AddrPortFrom(ip, addr.Port())
	}

	testcases := []struct {
		endpoint p2p.Endpoint
		ok       bool
	}{
		// Valid v4 and v6 addresses, with mconn and tcp protocols.
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Addr: reservePort(netip.IPv4Unspecified())}, true},
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Addr: reservePort(tcp.IPv4Loopback())}, true},
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Addr: reservePort(netip.IPv6Unspecified())}, true},
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Addr: reservePort(netip.IPv6Loopback())}, true},
		{p2p.Endpoint{Protocol: p2p.TCPProtocol, Addr: reservePort(netip.IPv4Unspecified())}, true},

		// Invalid endpoints.
		{p2p.Endpoint{}, false},
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Path: "foo"}, false},
		{p2p.Endpoint{Protocol: p2p.MConnProtocol, Addr: reservePort(netip.IPv4Unspecified()), Path: "foo"}, false},
	}
	for _, tc := range testcases {
		t.Run(tc.endpoint.String(), func(t *testing.T) {
			ctx := t.Context()
			t.Cleanup(leaktest.Check(t))

			transport := p2p.NewMConnTransport(
				log.NewNopLogger(),
				tc.endpoint,
				conn.DefaultMConnConfig(),
				[]*p2p.ChannelDescriptor{{ID: chID, Priority: 1}},
				p2p.MConnTransportOptions{},
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
