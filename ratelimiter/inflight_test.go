package ratelimiter

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func inFlightRegistry(t *testing.T, maxInFlight int) *Registry {
	t.Helper()
	r, err := New(Config{RPS: DefaultRPS, Burst: DefaultBurst, MaxInFlightPerIP: maxInFlight})
	require.NoError(t, err)
	return r
}

func TestAcquireInFlightCapsConcurrencyPerIP(t *testing.T) {
	r := inFlightRegistry(t, 2)
	ctx := context.Background()

	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	require.False(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))

	// The cap is per address, so a second IP has its own allowance.
	require.True(t, r.AcquireInFlight(ctx, "5.6.7.8", PlaneGRPC, "svc/M"))

	r.ReleaseInFlight("1.2.3.4")
	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
}

// TestReleaseInFlightDropsTheKey pins that the counter cannot grow with the
// number of addresses seen: an address holding nothing occupies no entry.
func TestReleaseInFlightDropsTheKey(t *testing.T) {
	r := inFlightRegistry(t, 2)
	ctx := context.Background()

	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	r.ReleaseInFlight("1.2.3.4")
	require.Equal(t, 1, r.InFlightHeld("1.2.3.4"))
	r.ReleaseInFlight("1.2.3.4")

	require.Equal(t, 0, r.InFlightHeld("1.2.3.4"))
	require.Empty(t, r.inflight.held)
}

// TestReleaseInFlightUnheldIsANoOp pins that an unmatched release cannot mint a
// slot, which would let an address exceed the cap.
func TestReleaseInFlightUnheldIsANoOp(t *testing.T) {
	r := inFlightRegistry(t, 1)
	ctx := context.Background()

	r.ReleaseInFlight("1.2.3.4")
	r.ReleaseInFlight("1.2.3.4")

	require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	require.False(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
}

// TestAcquireInFlightSharesAnIPv6Prefix holds the concurrency cap to the same
// keying as the buckets: rotating within a /64 does not buy a fresh allowance.
func TestAcquireInFlightSharesAnIPv6Prefix(t *testing.T) {
	r := inFlightRegistry(t, 1)
	ctx := context.Background()

	require.True(t, r.AcquireInFlight(ctx, "2001:db8::1", PlaneGRPC, "svc/M"))
	require.False(t, r.AcquireInFlight(ctx, "2001:db8::2", PlaneGRPC, "svc/M"))
	require.True(t, r.AcquireInFlight(ctx, "2001:db9::1", PlaneGRPC, "svc/M"))
}

// TestAcquireInFlightDisabledAdmitsEverything pins that a zero limit leaves the
// token bucket as the only admission control rather than blocking every RPC.
func TestAcquireInFlightDisabledAdmitsEverything(t *testing.T) {
	r := inFlightRegistry(t, 0)
	ctx := context.Background()

	for range 100 {
		require.True(t, r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M"))
	}
	require.Equal(t, 0, r.InFlightHeld("1.2.3.4"))
	r.ReleaseInFlight("1.2.3.4")
}

func TestAcquireInFlightIsConcurrencySafe(t *testing.T) {
	const max = 8
	r := inFlightRegistry(t, max)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.AcquireInFlight(ctx, "1.2.3.4", PlaneGRPC, "svc/M") {
				r.ReleaseInFlight("1.2.3.4")
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 0, r.InFlightHeld("1.2.3.4"))
}

func TestIsKnownGRPCMethod(t *testing.T) {
	r := inFlightRegistry(t, 1)

	// Nothing is known until the server has declared what it serves.
	require.False(t, r.IsKnownGRPCMethod("/testdata.Query/Echo"))

	r.SetKnownGRPCMethods([]string{"testdata.Query/Echo"})
	require.True(t, r.IsKnownGRPCMethod("/testdata.Query/Echo"))
	require.True(t, r.IsKnownGRPCMethod("testdata.Query/Echo"))
	require.False(t, r.IsKnownGRPCMethod("/testdata.Query/Nope"))
	require.False(t, r.IsKnownGRPCMethod("/nope"))
	require.False(t, r.IsKnownGRPCMethod(""))
}
