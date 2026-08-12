package p2p

import (
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// Every test harness pins MaxAcceptRate/MaxDialRate to rate.Inf and node setup
// always sets them, so these fallbacks are otherwise unexercised.
func TestRouterOptionsPacingDefaults(t *testing.T) {
	var o RouterOptions

	require.Equal(t, rate.Every(10*time.Millisecond), o.maxAcceptRate())
	require.Equal(t, rate.Every(10*time.Second), o.maxDialRate())

	// An explicit value wins over the fallback.
	o.MaxAcceptRate = utils.Some(rate.Inf)
	require.Equal(t, rate.Inf, o.maxAcceptRate())
}
