package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeeper_GetVoteTargets(t *testing.T) {
	input := CreateTestInput(t)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)

	expectedTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range expectedTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, target)
	}

	targets := input.OracleKeeper.GetVoteTargets(input.Ctx)
	require.Equal(t, expectedTargets, targets)
}

func TestKeeper_IsVoteTarget(t *testing.T) {
	input := CreateTestInput(t)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)

	validTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range validTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, target)
		require.True(t, input.OracleKeeper.IsVoteTarget(input.Ctx, target))
	}
}
