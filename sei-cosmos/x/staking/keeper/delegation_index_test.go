package keeper_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// seedDelegations writes delegations straight to the store, bypassing SetDelegation,
// so the index state under test is only what the migration produced.
func seedDelegations(t *testing.T, app *seiapp.App, ctx sdk.Context, delegations []types.Delegation) {
	t.Helper()
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	for _, delegation := range delegations {
		store.Set(
			types.GetDelegationKey(sdk.MustAccAddressFromBech32(delegation.DelegatorAddress), delegation.GetValidatorAddr()),
			types.MustMarshalDelegation(app.AppCodec(), delegation),
		)
	}
}

func TestDelegationByValIndexNotReadyNoDualWrite(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	require.False(t, app.StakingKeeper.DelegationByValIndexReady(ctx))

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
	require.NotNil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))
}

func TestDelegationByValIndexDualWriteAfterMigration(t *testing.T) {
	_, app, ctx := createTestInput(t)

	_, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	require.NoError(t, err)
	require.True(t, app.StakingKeeper.DelegationByValIndexReady(ctx))

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.True(t, store.Has(indexKey))
	require.NotNil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))

	app.StakingKeeper.RemoveDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
	require.Nil(t, store.Get(types.GetDelegationKey(addrDels[0], valAddrs[0])))
}

func TestMigrateDelegationByValIndex(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels, valAddrs := generateAddresses(app, ctx, 2)
	delegations := []types.Delegation{
		types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1)),
		types.NewDelegation(addrDels[0], valAddrs[1], sdk.NewDec(2)),
		types.NewDelegation(addrDels[1], valAddrs[0], sdk.NewDec(3)),
	}
	seedDelegations(t, app, ctx, delegations)

	result, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, result.TotalDelegations)
	require.Equal(t, 3, result.IndexWritten)
	require.False(t, result.AlreadyReady)
	require.True(t, app.StakingKeeper.DelegationByValIndexReady(ctx))

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	for _, delegation := range delegations {
		delAddr := sdk.MustAccAddressFromBech32(delegation.DelegatorAddress)
		require.True(t, store.Has(types.GetDelegationByValIndexKey(delAddr, delegation.GetValidatorAddr())))
	}

	repeat, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	require.NoError(t, err)
	require.True(t, repeat.AlreadyReady)
	require.Equal(t, 0, repeat.IndexWritten)
}

// TestMigrateDelegationByValIndexNoOrphans pins the invariant the index exists to
// uphold: every delegation is indexed, and every index entry resolves to a delegation.
func TestMigrateDelegationByValIndexNoOrphans(t *testing.T) {
	_, app, ctx := createTestInput(t)

	addrDels, valAddrs := generateAddresses(app, ctx, 3)
	seedDelegations(t, app, ctx, []types.Delegation{
		types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1)),
		types.NewDelegation(addrDels[1], valAddrs[0], sdk.NewDec(2)),
		types.NewDelegation(addrDels[2], valAddrs[1], sdk.NewDec(3)),
	})

	_, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	require.NoError(t, err)

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())

	delegationKeys := map[string]bool{}
	delIter := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	for ; delIter.Valid(); delIter.Next() {
		delegationKeys[string(delIter.Key())] = true
	}
	require.NoError(t, delIter.Close())

	indexed := map[string]bool{}
	idxIter := sdk.KVStorePrefixIterator(store, types.DelegationByValIndexKey)
	for ; idxIter.Valid(); idxIter.Next() {
		resolved := types.GetDelegationKeyFromValIndexKey(idxIter.Key())
		require.True(t, store.Has(resolved), "index entry resolves to no delegation")
		indexed[string(resolved)] = true
	}
	require.NoError(t, idxIter.Close())

	require.Equal(t, delegationKeys, indexed)
}

// maxScanDelegator sorts after every test delegator address, so a filtered scan for
// its validator must traverse the whole delegation keyspace to reach it.
var maxScanDelegator = sdk.AccAddress(bytes.Repeat([]byte{0xff}, 20))

// TestGRPCQueryValidatorDelegationsIndexedBeyondScanLimit pins the defect the index
// closes: past query.MaxScanLimit entries the filtered scan reverts, and reading the
// per-validator prefix answers the same request.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsIndexedBeyondScanLimit() {
	app, ctx := suite.app, suite.ctx
	querier := keeper.Querier{Keeper: app.StakingKeeper}

	targetVal := suite.vals[1].GetOperator()
	fillerVal := suite.vals[0].GetOperator()
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())

	for i := 0; i <= int(query.MaxScanLimit); i++ {
		delAddr := make(sdk.AccAddress, 20)
		binary.BigEndian.PutUint64(delAddr[12:], uint64(i))
		delegation := types.NewDelegation(delAddr, fillerVal, sdk.NewDec(1))
		store.Set(
			types.GetDelegationKey(delAddr, fillerVal),
			types.MustMarshalDelegation(app.AppCodec(), delegation),
		)
	}
	target := types.NewDelegation(maxScanDelegator, targetVal, sdk.NewDec(1))
	store.Set(
		types.GetDelegationKey(maxScanDelegator, targetVal),
		types.MustMarshalDelegation(app.AppCodec(), target),
	)

	req := &types.QueryValidatorDelegationsRequest{ValidatorAddr: targetVal.String()}

	// Before the migration the indexed query falls through to the scan, so it reverts
	// exactly as the query does at heights below the one that populated the index.
	_, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.Error(err)
	_, err = querier.ValidatorDelegationsIndexed(sdk.WrapSDKContext(ctx), req)
	suite.Error(err)

	_, err = app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	suite.NoError(err)

	res, err := querier.ValidatorDelegationsIndexed(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)
	delegators := make([]string, 0, len(res.DelegationResponses))
	for _, dr := range res.DelegationResponses {
		suite.Equal(targetVal.String(), dr.Delegation.ValidatorAddress)
		delegators = append(delegators, dr.Delegation.DelegatorAddress)
	}
	suite.Contains(delegators, maxScanDelegator.String())

	// The unindexed query is untouched, so the legacy precompiles that call it keep
	// the behavior they have today.
	_, err = querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.Error(err)
}

// TestGRPCQueryValidatorDelegationsIndexedMatchesScan pins that the two read paths
// agree on a validator small enough for both to answer.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsIndexedMatchesScan() {
	app, ctx := suite.app, suite.ctx
	querier := keeper.Querier{Keeper: app.StakingKeeper}

	req := &types.QueryValidatorDelegationsRequest{ValidatorAddr: suite.vals[1].GetOperator().String()}

	scanned, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)

	_, err = app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	suite.NoError(err)

	indexed, err := querier.ValidatorDelegationsIndexed(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)

	suite.ElementsMatch(scanned.DelegationResponses, indexed.DelegationResponses)
}
