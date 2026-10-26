package epoch

import (
	"encoding/hex"
	"testing"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestStateWindowChangeSetRoundTrip(t *testing.T) {
	c0 := testCommittee(t, map[string]uint64{"a": 1, "b": 2})
	c1, err := c0.DeriveNext(testWeights(c0), 1)
	require.NoError(t, err)
	c2, err := c1.DeriveNext(testWeights(c1), 2)
	require.NoError(t, err)

	window, err := NewStateWindow(0, c0, 1, c1, 2, c2)
	require.NoError(t, err)
	changeSet := window.ChangeSet()

	require.Equal(t, StateModuleName, changeSet.Name)
	require.Equal(t, []string{PrevEpochRecordKey, CurrentEpochRecordKey, NextEpochRecordKey}, pairKeys(changeSet))

	decoded, err := DecodeStateWindow(
		changeSet.Changeset.Pairs[0].Value,
		changeSet.Changeset.Pairs[1].Value,
		changeSet.Changeset.Pairs[2].Value,
	)
	require.NoError(t, err)
	require.True(t, window.Equal(decoded))
}

func TestStateWindowRejectsLateJoiner(t *testing.T) {
	c0 := testCommittee(t, map[string]uint64{"a": 1})
	c2, err := c0.DeriveNext(map[types.PublicKey]uint64{types.TestSecretKey("late").Public(): 1}, 2)
	require.NoError(t, err)

	_, err = NewStateWindow(0, c0, 1, c2, 2, c2)
	require.Error(t, err)
}

func TestStateWindowRejectsNonConsecutiveRecords(t *testing.T) {
	c := testCommittee(t, map[string]uint64{"a": 1})

	_, err := NewStateWindow(2, c, 4, c, 5, c)
	require.Error(t, err)

	_, err = NewStateWindow(2, c, 3, c, 5, c)
	require.Error(t, err)
}

func TestStateEpochRecordAllowsGenesisEpochs(t *testing.T) {
	c := testCommittee(t, map[string]uint64{"a": 1})
	next, err := c.DeriveNext(testWeights(c), 2)
	require.NoError(t, err)

	window, err := NewStateWindow(0, c, 1, c, 2, next)
	require.NoError(t, err)
	changeSet := window.ChangeSet()

	decoded, err := DecodeStateWindow(
		changeSet.Changeset.Pairs[0].Value,
		changeSet.Changeset.Pairs[1].Value,
		changeSet.Changeset.Pairs[2].Value,
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(0), decoded.Prev().Index)
	require.True(t, c.Equal(decoded.Prev().Committee))
}

func TestStateEpochRecordGoldenBytes(t *testing.T) {
	c := testCommittee(t, map[string]uint64{"a": 7})
	record := encodeEpochRecord(3, c)

	got := hex.EncodeToString(protoutils.Marshal(record))
	const want = "0803122c0a2a0a260a220a20eae1c8793b5597c4b3f490e76ac31172c439690f8ee14142bb851a61f9a49f0e10001007"
	require.Equal(t, want, got)
}

func testCommittee(t *testing.T, weightsBySeed map[string]uint64) *types.Committee {
	t.Helper()
	weights := make(map[types.PublicKey]uint64, len(weightsBySeed))
	for seed, weight := range weightsBySeed {
		weights[types.TestSecretKey(types.NodeID(seed)).Public()] = weight
	}
	committee, err := types.NewCommittee(weights)
	require.NoError(t, err)
	return committee
}

func testWeights(c *types.Committee) map[types.PublicKey]uint64 {
	weights := make(map[types.PublicKey]uint64, c.Lanes().Len())
	for lane := range c.Lanes().All() {
		weights[lane.Validator] = c.Weight(lane.Validator)
	}
	return weights
}

func pairKeys(changeSet *dbproto.NamedChangeSet) []string {
	keys := make([]string, len(changeSet.Changeset.Pairs))
	for i, pair := range changeSet.Changeset.Pairs {
		keys[i] = string(pair.Key)
	}
	return keys
}
