package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestDelegationByValIndexDualWrite(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	require.False(t, store.Has(indexKey))

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.True(t, store.Has(indexKey))
	require.NotNil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))

	app.StakingKeeper.RemoveDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
	require.Nil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))
}

func TestDelegationByValIndexTracingPreUpgradeNoDualWrite(t *testing.T) {
	_, app, ctx := createTestInput(t)
	ctx = ctx.WithIsTracing(true).WithClosestUpgradeName("v6.6")

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
	require.NotNil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))

	app.StakingKeeper.RemoveDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
	require.Nil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))
}

func TestDelegationByValIndexTracingPostUpgradeDualWrite(t *testing.T) {
	_, app, ctx := createTestInput(t)
	ctx = ctx.WithIsTracing(true).WithClosestUpgradeName("v6.7")

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.True(t, store.Has(indexKey))
}

func TestBackfillDelegationByValIndex(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels, valAddrs := generateAddresses(app, ctx, 2)
	delegations := []types.Delegation{
		types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1)),
		types.NewDelegation(addrDels[0], valAddrs[1], sdk.NewDec(2)),
		types.NewDelegation(addrDels[1], valAddrs[0], sdk.NewDec(3)),
	}

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	for _, delegation := range delegations {
		delegatorAddress := sdk.MustAccAddressFromBech32(delegation.DelegatorAddress)
		valAddr := delegation.GetValidatorAddr()
		store.Set(
			types.GetDelegationKey(delegatorAddress, valAddr),
			types.MustMarshalDelegation(app.AppCodec(), delegation),
		)
	}

	dryRunResult := app.StakingKeeper.BackfillDelegationByValIndex(ctx, true)
	require.Equal(t, 3, dryRunResult.TotalDelegations)
	require.Equal(t, 3, dryRunResult.IndexWritten)
	require.Equal(t, 0, dryRunResult.AlreadyIndexed)
	require.True(t, dryRunResult.DryRun)
	for _, delegation := range delegations {
		delegatorAddress := sdk.MustAccAddressFromBech32(delegation.DelegatorAddress)
		valAddr := delegation.GetValidatorAddr()
		require.False(t, store.Has(types.GetDelegationByValIndexKey(delegatorAddress, valAddr)))
	}

	writeResult := app.StakingKeeper.BackfillDelegationByValIndex(ctx, false)
	require.Equal(t, 3, writeResult.TotalDelegations)
	require.Equal(t, 3, writeResult.IndexWritten)
	require.Equal(t, 0, writeResult.AlreadyIndexed)
	require.False(t, writeResult.DryRun)
	for _, delegation := range delegations {
		delegatorAddress := sdk.MustAccAddressFromBech32(delegation.DelegatorAddress)
		valAddr := delegation.GetValidatorAddr()
		require.True(t, store.Has(types.GetDelegationByValIndexKey(delegatorAddress, valAddr)))
	}

	repeatResult := app.StakingKeeper.BackfillDelegationByValIndex(ctx, false)
	require.Equal(t, 3, repeatResult.TotalDelegations)
	require.Equal(t, 0, repeatResult.IndexWritten)
	require.Equal(t, 3, repeatResult.AlreadyIndexed)
}
