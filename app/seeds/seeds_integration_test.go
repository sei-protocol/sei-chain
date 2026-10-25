//go:build integration

// Reachability checks against the live published seed endpoints, build-tagged
// off by default because they make real network calls:
//
//	go test -tags=integration ./app/seeds/...
//
// Run in CI by .github/workflows/seed-reachability.yml.
package seeds

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/stretchr/testify/require"
)

const dialTimeout = 10 * time.Second

// maxUnreachablePerNetwork is how many seeds in one network may fail to speak
// before this suite fails.
//
// One rather than zero, because that is the property the seeds actually owe:
// three per network exist so losing a region does not cost bootstrap
// capability, and a node bootstraps fine on the remaining two. Failing on any
// single unreachable seed would make this a liveness alarm for individual pods
// rather than a check that the published set still does its job.
//
// A tolerated failure is still named in the output, so a seed that stays dark
// is visible rather than silently absorbed.
//
// Tighten this to zero once every published seed is serving.
const maxUnreachablePerNetwork = 1

// A seed that is reachable at the TCP layer but never speaks is the failure
// mode this exists for: the listener accepts, the pod reports Ready, seed mode
// publishes no metrics, and inbound is silently closed. Only the bytes on the
// wire distinguish that from a healthy seed, so assert them.
//
// A conforming node sends its ephemeral-key preface immediately on connect
// without waiting for the dialer, so a seed that sends nothing is broken
// regardless of why.
func TestSeedsAreReachableAndSpeakP2P(t *testing.T) {
	for chainID, addrs := range chainSeeds {
		t.Run(chainID, func(t *testing.T) {
			var unreachable []string

			for _, entry := range addrs {
				addr, err := config.ParseNodeAddress(entry)
				require.NoErrorf(t, err, "%s: %q", chainID, entry)

				if err := speaksP2P(addr); err != nil {
					unreachable = append(unreachable, fmt.Sprintf("%s: %v", addr.Hostname, err))
					t.Logf("UNREACHABLE  %s", addr.Hostname)
					continue
				}
				t.Logf("ok           %s", addr.Hostname)
			}

			require.LessOrEqualf(t, len(unreachable), maxUnreachablePerNetwork,
				"%s: %d of %d seeds are not serving P2P (tolerating up to %d):\n  %v",
				chainID, len(unreachable), len(addrs), maxUnreachablePerNetwork, unreachable)
		})
	}
}

// speaksP2P dials the seed and waits for it to send its handshake preface.
func speaksP2P(addr config.NodeAddress) error {
	hostPort := net.JoinHostPort(addr.Hostname, strconv.Itoa(int(addr.Port)))

	conn, err := net.DialTimeout("tcp", hostPort, dialTimeout)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(dialTimeout)); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	n, err := conn.Read(make([]byte, 64))
	if err != nil {
		return fmt.Errorf("accepted the connection but sent nothing: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("sent an empty preface")
	}
	return nil
}
