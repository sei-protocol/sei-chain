package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Test checking that stateless leader election is deterministic.
// Breaking this test automatically means that your change is protocol breaking.
func TestRoundStateLeaderChangeDetector(t *testing.T) {
	seeds := [10]string{
		"round-state-leader-0",
		"round-state-leader-1",
		"round-state-leader-2",
		"round-state-leader-3",
		"round-state-leader-4",
		"round-state-leader-5",
		"round-state-leader-6",
		"round-state-leader-7",
		"round-state-leader-8",
		"round-state-leader-9",
	}
	weights := [10]int64{5, 6, 7, 8, 9, 10, 5, 6, 7, 8}
	vals := make([]*tmtypes.Validator, len(seeds))
	for i, seed := range seeds {
		vals[i] = tmtypes.NewValidator(ed25519.TestSecretKey([]byte(seed)).Public(), weights[i])
	}

	rs := RoundState{
		Validators: tmtypes.NewValidatorSet(vals),
	}

	for _, tc := range []struct {
		height int64
		round  int32
		want   int
	}{
		{1, 0, 0},
		{1, 1, 0},
		{2, 0, 5},
		{2, 3, 3},
		{7, 2, 5},
		{11, 5, 6},
		{42, 0, 7},
		{42, 9, 2},
		{99, 4, 9},
		{123456, 7, 9},
	} {
		rs.Height, rs.Round = tc.height, tc.round
		require.Equal(t, rs.Validators.Validators[tc.want].PubKey, rs.Leader(), "height=%d round=%d", tc.height, tc.round)
	}
}

// TestRoundStateEmptyValidatorSet checks that the event/RPC serializers yield
// an empty proposer on an empty validator set rather than dividing by the set's
// zero total voting power in leader election.
func TestRoundStateEmptyValidatorSet(t *testing.T) {
	rs := RoundState{
		Validators: tmtypes.NewValidatorSet(nil),
	}
	rs.Height, rs.Round = 1, 0
	require.Zero(t, rs.Validators.TotalVotingPower())

	// The event/RPC serializers must yield an empty proposer rather than
	// dividing by zero in leader election.
	require.NotPanics(t, func() {
		ev := rs.NewRoundEvent()
		require.Zero(t, ev.Proposer.Index)
		require.Empty(t, ev.Proposer.Address)
	}, "NewRoundEvent must not divide by zero on an empty validator set")
}

func TestRoundStateLeaderDistribution(t *testing.T) {
	const samples = 100000

	rng := utils.TestRng()
	vals := make([]*tmtypes.Validator, 10)
	for i := range vals {
		vals[i] = tmtypes.NewValidator(ed25519.TestSecretKey(utils.GenBytes(rng, 32)).Public(), int64(i)+1)
	}
	vs := tmtypes.NewValidatorSet(vals)

	count := map[crypto.PubKey]int{}
	for h := range int64(samples) {
		count[getLeader(vs, h, 0)]++
	}
	require.NoError(t, checkDistribution(vs, count, 0.05))

	count = map[crypto.PubKey]int{}
	h := rng.Int63n(1_000_000) + 1
	for r := range int32(samples) {
		count[getLeader(vs, h, r)]++
	}
	require.NoError(t, checkDistribution(vs, count, 0.05))
}

func getLeader(vs *tmtypes.ValidatorSet, height int64, round int32) crypto.PubKey {
	return (&RoundState{
		Validators: vs,
		HRS: HRS{
			Height: height,
			Round:  round,
		},
	}).Leader()
}

func checkDistribution(vs *tmtypes.ValidatorSet, counts map[crypto.PubKey]int, tolerance float64) error {
	var totalWeight int64
	for _, val := range vs.Validators {
		totalWeight += val.VotingPower
	}

	var samples int
	for _, count := range counts {
		samples += count
	}

	for i, val := range vs.Validators {
		actual := counts[val.PubKey]
		expected := float64(samples) * float64(val.VotingPower) / float64(totalWeight)
		relativeError := math.Abs(float64(actual)-expected) / expected
		if relativeError >= tolerance {
			return fmt.Errorf(
				"validator %d distribution out of tolerance: expected=%f actual=%d relative_error=%f tolerance=%f",
				i,
				expected,
				actual,
				relativeError,
				tolerance,
			)
		}
	}

	return nil
}
