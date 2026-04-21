package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
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
		StatelessLeaderElection: true,
		Validators:              tmtypes.NewValidatorSet(vals),
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
