package ratelimiter

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// acceptedConns runs a listener's Accept loop into a channel so a test can see
// which connections the per-IP cap let through.
func acceptedConns(t *testing.T, ln net.Listener) <-chan net.Conn {
	t.Helper()
	out := make(chan net.Conn, 16)
	go func() {
		defer close(out)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			out <- conn
		}
	}()
	return out
}

func dialTo(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// requireClosedByServer pins that the server hung up on conn rather than serving
// it, which is how an over-cap connection is refused.
func requireClosedByServer(t *testing.T, conn net.Conn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := conn.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

func TestConnLimitListenerCapsConnectionsPerIP(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln := ConnLimitListener(raw, PlaneGRPC, 2)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := acceptedConns(t, ln)

	first := dialTo(t, ln)
	second := dialTo(t, ln)
	served := []net.Conn{<-accepted, <-accepted}

	// The third is dropped, and the loop keeps serving rather than failing.
	requireClosedByServer(t, dialTo(t, ln))
	require.Empty(t, accepted)

	// Closing a served connection returns its slot.
	require.NoError(t, served[0].Close())
	require.NoError(t, first.Close())
	dialTo(t, ln)
	require.NotNil(t, <-accepted)

	_ = second.Close()
	_ = served[1].Close()
}

// TestConnLimitListenerDoubleCloseReturnsOneSlot pins the idempotent release:
// net/http and grpc-go both close a connection from more than one path, and a
// second release would hand the address a slot it is not holding.
func TestConnLimitListenerDoubleCloseReturnsOneSlot(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln := ConnLimitListener(raw, PlaneGRPC, 1)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := acceptedConns(t, ln)

	dialTo(t, ln)
	served := <-accepted
	require.NoError(t, served.Close())
	_ = served.Close()

	limiter, ok := ln.(*connLimitListener)
	require.True(t, ok)
	require.Equal(t, 0, limiter.counter.heldFor("127.0.0.1"))

	// One slot came back, not two.
	dialTo(t, ln)
	require.NotNil(t, <-accepted)
	requireClosedByServer(t, dialTo(t, ln))
}

// TestConnLimitListenerDisabledReturnsInner pins that a non-positive cap adds no
// wrapper at all, so the listener behaves exactly as before.
func TestConnLimitListenerDisabledReturnsInner(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	require.Same(t, raw, ConnLimitListener(raw, PlaneGRPC, 0))
	require.Same(t, raw, ConnLimitListener(raw, PlaneGRPC, -1))
}

// TestConnLimitListenerAcceptStopsOnListenerError pins that a closed listener
// still ends the Accept loop rather than spinning inside it.
func TestConnLimitListenerAcceptStopsOnListenerError(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln := ConnLimitListener(raw, PlaneGRPC, 1)
	require.NoError(t, ln.Close())

	_, err = ln.Accept()
	require.Error(t, err)
}
