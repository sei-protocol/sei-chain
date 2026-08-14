package p2p

import (
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// #3922 removed the package-level pacing fallbacks in favour of requiring both
// rates from the construction site, so the risk this guards has moved rather
// than gone: a site that forgets to set them no longer inherits a rate too low
// to drain the listen backlog, it fails Validate. Every router test harness
// pins these to rate.Inf and node setup derives them from config, so nothing
// else exercises the unset case.
func TestRouterOptionsRequirePacingRates(t *testing.T) {
	var o RouterOptions
	require.Error(t, o.Validate())

	o.MaxDialRate = rate.Every(10 * time.Second)
	require.Error(t, o.Validate())

	o.MaxAcceptRate = rate.Every(10 * time.Millisecond)
	require.NoError(t, o.Validate())

	// The accessors are plain reads now; no fallback may reappear between the
	// field and the limiter.
	require.Equal(t, rate.Every(10*time.Millisecond), o.maxAcceptRate())
	require.Equal(t, rate.Every(10*time.Second), o.maxDialRate())

	// accept-interval = 0 is the documented way to disable pacing: rate.Every
	// maps a non-positive interval to rate.Inf, which Validate still accepts.
	o.MaxAcceptRate = rate.Every(0)
	require.Equal(t, rate.Inf, o.maxAcceptRate())
	require.NoError(t, o.Validate())
}
