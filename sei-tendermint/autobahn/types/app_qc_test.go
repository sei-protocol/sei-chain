package types

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestAppQCVerifyChecksEpochAndRoad(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := NewEpoch(1, RoadRange{First: 100, Next: 200}, time.Time{}, committee, 1)

	ok := makeAppQCFor(keys, 0, 150, GenAppHash(rng), 1)
	require.NoError(t, ok.Verify(ep))

	wrongEpoch := makeAppQCFor(keys, 0, 150, GenAppHash(rng), 0)
	require.Error(t, wrongEpoch.Verify(ep))

	wrongRoad := makeAppQCFor(keys, 0, 50, GenAppHash(rng), 1)
	require.Error(t, wrongRoad.Verify(ep))
}
