package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/stretchr/testify/require"
)

func TestWithDependencyMappingGenerator(t *testing.T) {
	var testKeeper keeper.Keeper
	generator := make(keeper.DependencyGeneratorMap)
	generator["test"] = func(keeper keeper.Keeper, ctx types.Context, msg types.Msg) ([]acltypes.AccessOperation, error) {
		return []acltypes.AccessOperation{}, nil
	}
	apply := keeper.WithDependencyMappingGenerator(generator)
	apply.Apply(&testKeeper)

	require.Equal(t, generator, testKeeper.MessageDependencyGeneratorMapper)
}

func TestWithDependencyGeneratorMappings(t *testing.T) {
	var testKeeper keeper.Keeper
	generator := make(keeper.DependencyGeneratorMap)
	generator["test"] = func(keeper keeper.Keeper, ctx types.Context, msg types.Msg) ([]acltypes.AccessOperation, error) {
		return []acltypes.AccessOperation{}, nil
	}
	testKeeper.MessageDependencyGeneratorMapper = generator
	newGenerator := make(keeper.DependencyGeneratorMap)
	newGenerator["newTest"] = func(keeper keeper.Keeper, ctx types.Context, msg types.Msg) ([]acltypes.AccessOperation, error) {
		return []acltypes.AccessOperation{}, nil
	}

	require.True(t, testKeeper.MessageDependencyGeneratorMapper.Contains("test"))
	require.False(t, testKeeper.MessageDependencyGeneratorMapper.Contains("newTest"))
	apply := keeper.WithDependencyGeneratorMappings(newGenerator)
	apply.Apply(&testKeeper)
	require.True(t, testKeeper.MessageDependencyGeneratorMapper.Contains("test"))
	require.True(t, testKeeper.MessageDependencyGeneratorMapper.Contains("newTest"))

}

func TestDependencyGeneratorMap_Merge(t *testing.T) {
	oldGenerator := make(keeper.DependencyGeneratorMap)
	oldGenerator["oldTest"] = func(keeper keeper.Keeper, ctx types.Context, msg types.Msg) ([]acltypes.AccessOperation, error) {
		return []acltypes.AccessOperation{}, nil
	}
	newGenerator := make(keeper.DependencyGeneratorMap)
	newGenerator["newTest"] = func(keeper keeper.Keeper, ctx types.Context, msg types.Msg) ([]acltypes.AccessOperation, error) {
		return []acltypes.AccessOperation{}, nil
	}

	mergedGenerators := oldGenerator.Merge(newGenerator)
	require.True(t, mergedGenerators.Contains("oldTest"))
	require.True(t, mergedGenerators.Contains("newTest"))
}
