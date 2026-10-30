package keeper_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// unreadyDelegationByValIndex deletes the index and its markers, leaving the store as
// a chain that has not run the migration holds it.
func unreadyDelegationByValIndex(t *testing.T, app *seiapp.App, ctx sdk.Context) {
	t.Helper()
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	iterator := sdk.KVStorePrefixIterator(store, types.DelegationByValIndexKey)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, iterator.Key())
	}
	require.NoError(t, iterator.Close())
	for _, key := range keys {
		store.Delete(key)
	}
	store.Delete(types.DelegationByValIndexReadyKey)
	store.Delete(types.DelegationByValIndexCursorKey)
	require.False(t, app.StakingKeeper.DelegationByValIndexReady(ctx))
}

func TestDelegationByValIndexNotReadyNoDualWrite(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)

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
	unreadyDelegationByValIndex(t, app, ctx)

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
	unreadyDelegationByValIndex(t, app, ctx)
	genesisDelegations := len(app.StakingKeeper.GetAllDelegations(ctx))

	addrDels, valAddrs := generateAddresses(app, ctx, 2)
	delegations := []types.Delegation{
		types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1)),
		types.NewDelegation(addrDels[0], valAddrs[1], sdk.NewDec(2)),
		types.NewDelegation(addrDels[1], valAddrs[0], sdk.NewDec(3)),
	}
	seedDelegations(t, app, ctx, delegations)

	result, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	require.NoError(t, err)
	require.Equal(t, genesisDelegations+3, result.TotalDelegations)
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
	require.Zero(t, repeat.TotalDelegations)
}

// TestMigrateDelegationByValIndexNoOrphans pins the invariant the index exists to
// uphold: every delegation is indexed, and every index entry resolves to a delegation.
func TestMigrateDelegationByValIndexNoOrphans(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)

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

// delegatorAt returns a delegator address whose last byte is b and whose other bytes
// are fill, so its delegation keys sort by fill and then b.
func delegatorAt(fill, b byte) sdk.AccAddress {
	addr := sdk.AccAddress(bytes.Repeat([]byte{fill}, 20))
	addr[19] = b
	return addr
}

// requireIndexMatchesDelegations requires the index to hold exactly one entry per
// stored delegation.
func requireIndexMatchesDelegations(t *testing.T, store sdk.KVStore) {
	t.Helper()
	delegationKeys := map[string]bool{}
	delIter := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	for ; delIter.Valid(); delIter.Next() {
		delegationKeys[string(delIter.Key())] = true
	}
	require.NoError(t, delIter.Close())

	indexed := map[string]bool{}
	idxIter := sdk.KVStorePrefixIterator(store, types.DelegationByValIndexKey)
	for ; idxIter.Valid(); idxIter.Next() {
		indexed[string(types.GetDelegationKeyFromValIndexKey(idxIter.Key()))] = true
	}
	require.NoError(t, idxIter.Close())

	require.Equal(t, delegationKeys, indexed)
}

func TestStartDelegationByValIndexBackfillMaintainsWrites(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)

	app.StakingKeeper.StartDelegationByValIndexBackfill(ctx)
	require.False(t, app.StakingKeeper.DelegationByValIndexReady(ctx))

	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	delegation := types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	indexKey := types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])

	app.StakingKeeper.SetDelegation(ctx, delegation)
	require.True(t, store.Has(indexKey))

	app.StakingKeeper.RemoveDelegation(ctx, delegation)
	require.False(t, store.Has(indexKey))
}

func TestStartDelegationByValIndexBackfillOnceReady(t *testing.T) {
	_, app, ctx := createTestInput(t)
	require.True(t, app.StakingKeeper.DelegationByValIndexReady(ctx))

	app.StakingKeeper.StartDelegationByValIndexBackfill(ctx)

	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	require.False(t, store.Has(types.DelegationByValIndexCursorKey))
	indexed, err := app.StakingKeeper.BackfillDelegationByValIndex(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, indexed)
}

func TestBackfillDelegationByValIndexNotStarted(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)

	indexed, err := app.StakingKeeper.BackfillDelegationByValIndex(ctx, 100)
	require.NoError(t, err)
	require.Zero(t, indexed)
	require.False(t, app.StakingKeeper.DelegationByValIndexReady(ctx))
}

func TestBackfillDelegationByValIndexRejectsNonPositiveLimit(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)
	addrDels, valAddrs := generateAddresses(app, ctx, 1)
	seedDelegations(t, app, ctx, []types.Delegation{types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))})
	app.StakingKeeper.StartDelegationByValIndexBackfill(ctx)

	for _, limit := range []int{0, -1} {
		indexed, err := app.StakingKeeper.BackfillDelegationByValIndex(ctx, limit)
		require.Error(t, err)
		require.Zero(t, indexed)
	}
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	require.Equal(t, []byte{}, store.Get(types.DelegationByValIndexCursorKey))
	require.False(t, store.Has(types.GetDelegationByValIndexKey(addrDels[0], valAddrs[0])))
}

// TestBackfillDelegationByValIndexInBatches pins that the backfill indexes at most
// limit delegations per call, and that delegations written, changed or removed while it
// runs, on either side of the cursor, leave the finished index exact.
func TestBackfillDelegationByValIndexInBatches(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())
	_, valAddrs := generateAddresses(app, ctx, 1)
	valAddr := valAddrs[0]

	// The 0x00 delegators sort before every other delegation and the 0xff ones after.
	first, second := delegatorAt(0x00, 1), delegatorAt(0x00, 2)
	ahead, last := delegatorAt(0xff, 0xfe), delegatorAt(0xff, 0xff)
	seedDelegations(t, app, ctx, []types.Delegation{
		types.NewDelegation(first, valAddr, sdk.NewDec(1)),
		types.NewDelegation(second, valAddr, sdk.NewDec(2)),
		types.NewDelegation(ahead, valAddr, sdk.NewDec(3)),
		types.NewDelegation(last, valAddr, sdk.NewDec(4)),
	})
	total := len(app.StakingKeeper.GetAllDelegations(ctx))

	app.StakingKeeper.StartDelegationByValIndexBackfill(ctx)
	indexed, err := app.StakingKeeper.BackfillDelegationByValIndex(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, 2, indexed)
	require.False(t, app.StakingKeeper.DelegationByValIndexReady(ctx))
	require.Equal(t, types.GetDelegationKey(second, valAddr), store.Get(types.DelegationByValIndexCursorKey))
	require.True(t, store.Has(types.GetDelegationByValIndexKey(first, valAddr)))
	require.False(t, store.Has(types.GetDelegationByValIndexKey(last, valAddr)))

	// Behind the cursor: a removal of an indexed delegation and a new delegation.
	app.StakingKeeper.RemoveDelegation(ctx, types.NewDelegation(first, valAddr, sdk.NewDec(1)))
	behind := delegatorAt(0x00, 0)
	app.StakingKeeper.SetDelegation(ctx, types.NewDelegation(behind, valAddr, sdk.NewDec(5)))
	// Ahead of the cursor: a change and a removal of delegations not yet indexed.
	app.StakingKeeper.SetDelegation(ctx, types.NewDelegation(ahead, valAddr, sdk.NewDec(6)))
	app.StakingKeeper.RemoveDelegation(ctx, types.NewDelegation(last, valAddr, sdk.NewDec(4)))

	remaining := total - 2 - 1 // the two indexed and the one removed ahead of the cursor
	for calls := 0; !app.StakingKeeper.DelegationByValIndexReady(ctx); calls++ {
		require.Less(t, calls, total, "the backfill did not finish")
		indexed, err = app.StakingKeeper.BackfillDelegationByValIndex(ctx, 2)
		require.NoError(t, err)
		require.LessOrEqual(t, indexed, 2)
		remaining -= indexed
	}
	require.Zero(t, remaining)
	require.False(t, store.Has(types.DelegationByValIndexCursorKey))
	require.False(t, store.Has(types.GetDelegationByValIndexKey(first, valAddr)))
	require.False(t, store.Has(types.GetDelegationByValIndexKey(last, valAddr)))
	requireIndexMatchesDelegations(t, store)
}

// TestBackfillDelegationByValIndexEndsOnBatchBoundary pins that a batch that indexes
// the last delegation finishes the backfill without a further call.
func TestBackfillDelegationByValIndexEndsOnBatchBoundary(t *testing.T) {
	_, app, ctx := createTestInput(t)
	unreadyDelegationByValIndex(t, app, ctx)
	addrDels, valAddrs := generateAddresses(app, ctx, 2)
	seedDelegations(t, app, ctx, []types.Delegation{
		types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1)),
		types.NewDelegation(addrDels[1], valAddrs[1], sdk.NewDec(2)),
	})
	total := len(app.StakingKeeper.GetAllDelegations(ctx))

	app.StakingKeeper.StartDelegationByValIndexBackfill(ctx)
	indexed, err := app.StakingKeeper.BackfillDelegationByValIndex(ctx, total)
	require.NoError(t, err)
	require.Equal(t, total, indexed)
	require.True(t, app.StakingKeeper.DelegationByValIndexReady(ctx))
	requireIndexMatchesDelegations(t, ctx.KVStore(app.StakingKeeper.GetStoreKey()))
}

// maxScanDelegator sorts after every test delegator address, so a filtered scan for
// its validator must traverse the whole delegation keyspace to reach it.
var maxScanDelegator = sdk.AccAddress(bytes.Repeat([]byte{0xff}, 20))

// TestGRPCQueryValidatorDelegationsBeyondScanLimit pins the defect the index closes:
// past query.MaxScanLimit entries the filtered scan reverts, and once the index is
// ready the same request is answered from the per-validator prefix.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsBeyondScanLimit() {
	app, ctx := suite.app, suite.ctx
	querier := keeper.Querier{Keeper: app.StakingKeeper}
	unreadyDelegationByValIndex(suite.T(), app, ctx)

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

	// Before the migration the query scans, so it reverts exactly as it does at
	// heights below the one that populated the index.
	_, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.Error(err)

	_, err = app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	suite.NoError(err)

	res, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)
	delegators := make([]string, 0, len(res.DelegationResponses))
	for _, dr := range res.DelegationResponses {
		suite.Equal(targetVal.String(), dr.Delegation.ValidatorAddress)
		delegators = append(delegators, dr.Delegation.DelegatorAddress)
	}
	suite.Contains(delegators, maxScanDelegator.String())
}

// TestGRPCQueryValidatorDelegationsIndexMatchesScan pins that the scan and the index
// agree on a validator small enough for both to answer.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsIndexMatchesScan() {
	app, ctx := suite.app, suite.ctx
	querier := keeper.Querier{Keeper: app.StakingKeeper}

	unreadyDelegationByValIndex(suite.T(), app, ctx)
	req := &types.QueryValidatorDelegationsRequest{ValidatorAddr: suite.vals[1].GetOperator().String()}

	scanned, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)

	_, err = app.StakingKeeper.MigrateDelegationByValIndex(ctx)
	suite.NoError(err)

	indexed, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), req)
	suite.NoError(err)

	suite.ElementsMatch(scanned.DelegationResponses, indexed.DelegationResponses)
}

// TestGRPCQueryValidatorDelegationsScanCursorAfterIndexReady pins that a forward page
// resumed from a key the scan issued returns, once the index is ready, what the scan
// would have returned, and that a reverse one is refused.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsScanCursorAfterIndexReady() {
	for _, abciQuery := range []bool{false, true} {
		suite.Run(fmt.Sprintf("abciQuery=%v", abciQuery), func() {
			suite.SetupTest()
			app, ctx := suite.app, suite.ctx.WithIsABCIQuery(abciQuery)
			querier := keeper.Querier{Keeper: app.StakingKeeper}
			unreadyDelegationByValIndex(suite.T(), app, ctx)

			valAddr := suite.vals[1].GetOperator()
			for b := byte(1); b <= 6; b++ {
				app.StakingKeeper.SetDelegation(ctx, types.NewDelegation(delegatorAt(0x00, b), valAddr, sdk.NewDec(1)))
			}
			shared := delegatorAt(0x00, 3)
			for _, val := range suite.vals {
				app.StakingKeeper.SetDelegation(ctx, types.NewDelegation(shared, val.GetOperator(), sdk.NewDec(1)))
			}

			page := func(pageReq *query.PageRequest) ([]string, []byte, error) {
				res, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), &types.QueryValidatorDelegationsRequest{
					ValidatorAddr: valAddr.String(), Pagination: pageReq,
				})
				if err != nil {
					return nil, nil, err
				}
				delegators := make([]string, 0, len(res.DelegationResponses))
				for _, dr := range res.DelegationResponses {
					delegators = append(delegators, dr.Delegation.DelegatorAddress)
				}
				return delegators, res.Pagination.NextKey, nil
			}

			_, firstPageCursor, err := page(&query.PageRequest{Limit: 2})
			suite.Require().NoError(err)
			cursors := [][]byte{firstPageCursor}
			for _, val := range suite.vals {
				cursors = append(cursors, types.GetDelegationKey(shared, val.GetOperator())[len(types.DelegationKey):])
			}
			scanned := make([][]string, len(cursors))
			for i, cursor := range cursors {
				scanned[i], _, err = page(&query.PageRequest{Key: cursor})
				suite.Require().NoError(err)
			}
			_, reverseCursor, err := page(&query.PageRequest{Limit: 2, Reverse: true})
			suite.Require().NoError(err)

			_, err = app.StakingKeeper.MigrateDelegationByValIndex(ctx)
			suite.Require().NoError(err)

			for i, cursor := range cursors {
				indexed, _, err := page(&query.PageRequest{Key: cursor})
				suite.Require().NoError(err)
				suite.Require().Equal(scanned[i], indexed, "cursor %X", cursor)
			}
			_, _, err = page(&query.PageRequest{Key: reverseCursor, Reverse: true})
			suite.Require().Equal(codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestGRPCQueryValidatorDelegationsOrphanIndexEntry pins that an index entry without
// its delegation fails the query rather than panicking it.
func (suite *KeeperTestSuite) TestGRPCQueryValidatorDelegationsOrphanIndexEntry() {
	app, ctx := suite.app, suite.ctx
	suite.Require().True(app.StakingKeeper.DelegationByValIndexReady(ctx))
	valAddr := suite.vals[0].GetOperator()
	ctx.KVStore(app.StakingKeeper.GetStoreKey()).Set(types.GetDelegationByValIndexKey(delegatorAt(0x00, 1), valAddr), []byte{})

	querier := keeper.Querier{Keeper: app.StakingKeeper}
	_, err := querier.ValidatorDelegations(sdk.WrapSDKContext(ctx), &types.QueryValidatorDelegationsRequest{ValidatorAddr: valAddr.String()})
	suite.Require().Equal(codes.Internal, status.Code(err))
}
