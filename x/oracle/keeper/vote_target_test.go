package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestKeeper_GetVoteTargets(t *testing.T) {
	input := CreateTestInput(t)

	input.OracleKeeper.ClearTobinTaxes(input.Ctx)

	expectedTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range expectedTargets {
		input.OracleKeeper.SetTobinTax(input.Ctx, target, sdk.OneDec())
	}

	targets := input.OracleKeeper.GetVoteTargets(input.Ctx)
	require.Equal(t, expectedTargets, targets)
}

func TestKeeper_IsVoteTarget(t *testing.T) {
	input := CreateTestInput(t)

	input.OracleKeeper.ClearTobinTaxes(input.Ctx)

	validTargets := []string{"bar", "foo", "whoowhoo"}
	for _, target := range validTargets {
		input.OracleKeeper.SetTobinTax(input.Ctx, target, sdk.OneDec())
		require.True(t, input.OracleKeeper.IsVoteTarget(input.Ctx, target))
	}
}
