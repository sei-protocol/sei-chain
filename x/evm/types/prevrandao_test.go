package types_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// prevRandaoFixture is the PREVRANDAO of prevRandaoFixtureTime. The literal pins the derivation:
// changing it changes block execution and forks the chain.
var prevRandaoFixture = common.HexToHash("0x753efd32f4a9d19b90b63f109cd4674b3104591ac57d57627625d92751ed8a50")

var prevRandaoFixtureTime = time.Date(2026, 1, 2, 14, 28, 1, 540537286, time.UTC)

func TestPrevRandao(t *testing.T) {
	require.Equal(t, prevRandaoFixture, types.PrevRandao(prevRandaoFixtureTime))
}

// TestPrevRandaoIgnoresLocation pins that one instant has one PREVRANDAO whatever zone it is
// expressed in, including a zero-offset zone that is not time.UTC.
func TestPrevRandaoIgnoresLocation(t *testing.T) {
	for name, loc := range map[string]*time.Location{
		"zero-offset zone": time.FixedZone("UTC", 0),
		"positive offset":  time.FixedZone("CEST", 2*60*60),
		"negative offset":  time.FixedZone("PST", -8*60*60),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, prevRandaoFixture, types.PrevRandao(prevRandaoFixtureTime.In(loc)))
		})
	}
}
