//go:build integration

// Build-tagged off by default: this makes real network calls to the published
// seed endpoints, so it belongs on CI (and on demand), not in the unit suite.
//
//	go test -tags=integration ./app/seeds/...
package seeds

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/stretchr/testify/require"
)

const dialTimeout = 10 * time.Second

// A seed that is reachable at the TCP layer but never speaks is the failure
// mode this test exists for: the listener accepts, the pod reports Ready, seed
// mode publishes no metrics, and inbound is silently closed. Only the bytes on
// the wire distinguish that from a healthy seed, so assert them.
//
// A conforming node sends its ephemeral-key preface immediately on connect
// without waiting for the dialer, so a seed that sends nothing is broken
// regardless of why. Everything that can be checked without a network lives in
// seeds_test.go, which runs by default.
func TestSeedsAreReachableAndSpeakP2P(t *testing.T) {
	for chainID, addrs := range chainSeeds {
		for _, entry := range addrs {
			addr, err := config.ParseNodeAddress(entry)
			require.NoErrorf(t, err, "%s: %q", chainID, entry)

			hostPort := net.JoinHostPort(addr.Hostname, strconv.Itoa(int(addr.Port)))
			t.Run(chainID+"/"+addr.Hostname, func(t *testing.T) {
				conn, err := net.DialTimeout("tcp", hostPort, dialTimeout)
				require.NoErrorf(t, err, "could not connect to %s", hostPort)
				defer conn.Close()

				require.NoError(t, conn.SetReadDeadline(time.Now().Add(dialTimeout)))
				buf := make([]byte, 64)
				n, err := conn.Read(buf)
				require.NoErrorf(t, err,
					"%s accepted the connection but sent nothing: inbound P2P is closed even though the listener is up", hostPort)
				require.NotZerof(t, n, "%s sent an empty preface", hostPort)
			})
		}
	}
}
