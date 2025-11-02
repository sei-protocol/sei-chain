package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/stretchr/testify/require"
)

func TestKeeper_GetVoteTargets(t *testing.T) {
	input := testutils.CreateTestInput(t)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)

	expectedTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range expectedTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, target)
	}

	targets := input.OracleKeeper.GetVoteTargets(input.Ctx)
	require.Equal(t, expectedTargets, targets)
}

func TestKeeper_IsVoteTarget(t *testing.T) {
	input := testutils.CreateTestInput(t)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)

	validTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range validTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, target)
		require.True(t, input.OracleKeeper.IsVoteTarget(input.Ctx, target))
	}
}
