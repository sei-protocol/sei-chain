package p2p

import (
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var localAddrCounter atomic.Uint32

func randLocalAddr() netip.AddrPort {
	n := localAddrCounter.Add(1)
	return netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, byte(n >> 16), byte(n >> 8), byte(n)}),
		uint16(n),
	)
}

func TestConnTracker(t *testing.T) {
	for name, factory := range map[string]func() *connTracker{
		"BaseSmall": func() *connTracker {
			return newConnTracker(10, time.Second)
		},
		"BaseLarge": func() *connTracker {
			return newConnTracker(100, time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			factory := factory // nolint:scopelint
			t.Run("Initialized", func(t *testing.T) {
				ct := factory()
				require.Equal(t, 0, ct.Len())
			})
			t.Run("RepeatedAdding", func(t *testing.T) {
				ct := factory()
				ip := randLocalAddr()
				require.NoError(t, ct.AddConn(ip))
				for i := 0; i < 100; i++ {
					_ = ct.AddConn(ip)
				}
				require.Equal(t, 1, ct.Len())
			})
			t.Run("AddingMany", func(t *testing.T) {
				ct := factory()
				for i := 0; i < 100; i++ {
					_ = ct.AddConn(randLocalAddr())
				}
				require.Equal(t, 100, ct.Len())
			})
			t.Run("Cycle", func(t *testing.T) {
				ct := factory()
				for i := 0; i < 100; i++ {
					ip := randLocalAddr()
					require.NoError(t, ct.AddConn(ip))
					ct.RemoveConn(ip)
				}
				require.Equal(t, 0, ct.Len())
			})
		})
	}
	t.Run("VeryShort", func(t *testing.T) {
		ct := newConnTracker(10, time.Microsecond)
		for i := 0; i < 10; i++ {
			ip := randLocalAddr()
			require.NoError(t, ct.AddConn(ip))
			time.Sleep(2 * time.Microsecond)
			require.NoError(t, ct.AddConn(ip))
		}
		require.Equal(t, 10, ct.Len())
	})
	t.Run("Window", func(t *testing.T) {
		const window = 100 * time.Millisecond
		ct := newConnTracker(10, window)
		ip := randLocalAddr()
		require.NoError(t, ct.AddConn(ip))
		ct.RemoveConn(ip)
		require.Error(t, ct.AddConn(ip))
		time.Sleep(window)
		require.NoError(t, ct.AddConn(ip))
	})

}

// A connection that dies inside the window keeps its lastConnect entry, because
// the window has not elapsed and a reconnect still has to be refused. Nothing
// revisited those entries afterwards, so on a public listener every address whose
// connection was short-lived stayed in the map for the life of the process.
func TestConnTrackerShortLivedConnsDoNotAccumulate(t *testing.T) {
	const conns = 100_000

	ct := newConnTracker(10, time.Millisecond)
	for range conns {
		ip := randLocalAddr()
		require.NoError(t, ct.AddConn(ip))
		ct.RemoveConn(ip)
	}

	// Bounded by the addresses seen within one window rather than by every address
	// seen. The margin is wide because the sweep is driven by elapsed time.
	require.Less(t, len(ct.lastConnect), conns/10)
}

// Reclaiming entries must not let an address reconnect inside its window.
func TestConnTrackerSweepPreservesWindow(t *testing.T) {
	ct := newConnTracker(10, time.Hour)
	ip := randLocalAddr()

	require.NoError(t, ct.AddConn(ip))
	ct.RemoveConn(ip)
	require.Error(t, ct.AddConn(ip))
}
