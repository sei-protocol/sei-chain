package dex_test

import (
	"context"
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/store"
	"github.com/stretchr/testify/require"
)

const (
	TEST_CONTRACT = "test"
)

func TestDeepCopy(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateTwo := stateOne.DeepCopy()
	cachedCtx, _ := store.GetCachedContext(ctx)
	stateTwo.GetBlockOrders(cachedCtx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           2,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	// old state must not be changed
	require.Equal(t, 1, len(stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
	// new state must be changed
	require.Equal(t, 2, len(stateTwo.GetBlockOrders(cachedCtx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
}

func TestDeepFilterAccounts(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           2,
		Account:      "test2",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:      1,
		Creator: "test",
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:      2,
		Creator: "test2",
	})
	stateOne.GetDepositInfo(ctx, types.ContractAddress(TEST_CONTRACT)).Add(&types.DepositInfoEntry{
		Creator: "test",
	})
	stateOne.GetDepositInfo(ctx, types.ContractAddress(TEST_CONTRACT)).Add(&types.DepositInfoEntry{
		Creator: "test2",
	})

	stateOne.DeepFilterAccount(ctx, "test")
	require.Equal(t, 1, len(stateOne.GetAllBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT))))
	require.Equal(t, 1, len(stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
	require.Equal(t, 1, len(stateOne.GetDepositInfo(ctx, types.ContractAddress(TEST_CONTRACT)).Get()))
}

func TestDeepDelete(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	dex.DeepDelete(ctx.KVStore(keeper.GetStoreKey()), types.KeyPrefix(types.MemOrderKey), func(_ []byte) bool { return true })
}

func TestClear(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           2,
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.Clear(ctx)
	require.Equal(t, 0, len(stateOne.GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
	require.Equal(t, 0, len(stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
}

func TestClearCancellationForPair(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           1,
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   "USDC",
		AssetDenom:   "ATOM",
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           2,
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   "USDC",
		AssetDenom:   "ATOM",
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           3,
		ContractAddr: TEST_CONTRACT,
		PriceDenom:   "USDC",
		AssetDenom:   "SEI",
	})
	stateOne.ClearCancellationForPair(ctx, TEST_CONTRACT, types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	require.Equal(t, 1, len(stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()))
	require.Equal(t, uint64(3), stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()[0].Id)
}

func TestSynchronization(t *testing.T) {
	k, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(k.GetMemStoreKey())
	targetContract := types.ContractAddress(TEST_CONTRACT)
	// no go context
	require.NotPanics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// no executing contract
	goCtx := context.Background()
	ctx = ctx.WithContext(goCtx)
	require.NotPanics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// executing contract same as target contract
	executingContract := types.ContractInfoV2{ContractAddr: TEST_CONTRACT}
	ctx = ctx.WithContext(context.WithValue(goCtx, dex.CtxKeyExecutingContract, executingContract))
	require.NotPanics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// executing contract attempting to access non-dependency
	executingContract.ContractAddr = "executing"
	ctx = ctx.WithContext(context.WithValue(goCtx, dex.CtxKeyExecutingContract, executingContract))
	require.Panics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// no termination map
	executingContract.Dependencies = []*types.ContractDependencyInfo{
		{Dependency: TEST_CONTRACT, ImmediateElderSibling: "elder"},
	}
	ctx = ctx.WithContext(context.WithValue(goCtx, dex.CtxKeyExecutingContract, executingContract))
	require.Panics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// no termination signal channel for sibling
	terminationSignals := datastructures.NewTypedSyncMap[string, chan struct{}]()
	goCtx = context.WithValue(goCtx, dex.CtxKeyExecutingContract, executingContract)
	goCtx = context.WithValue(goCtx, dex.CtxKeyExecTermSignal, terminationSignals)
	ctx = ctx.WithContext(goCtx)
	require.Panics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// termination signal times out
	siblingChan := make(chan struct{}, 1)
	terminationSignals.Store("elder", siblingChan)
	require.Panics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	// termination signal sent
	go func() {
		siblingChan <- struct{}{}
	}()
	require.NotPanics(t, func() { stateOne.SynchronizeAccess(ctx, targetContract) })
	<-siblingChan // the channel should be re-populated
}

func TestGetAllDownstreamContracts(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei1cnuw3f076wgdyahssdkd0g3nr96ckq8cwa2mh029fn5mgf2fmcmsae2elf",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "sei1ery8l6jquynn9a4cz2pff6khg8c68f7urt33l5n9dng2cwzz4c4q4hncrd",
			},
		},
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei1ery8l6jquynn9a4cz2pff6khg8c68f7urt33l5n9dng2cwzz4c4q4hncrd",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "sei1wl59k23zngj34l7d42y9yltask7rjlnxgccawc7ltrknp6n52fpsj6ctln",
			}, {
				Dependency: "sei1udfs22xpxle475m2nz7u47jfa3vngncdegmczwwdx00cmetypa3sman25q",
			},
		},
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei1wl59k23zngj34l7d42y9yltask7rjlnxgccawc7ltrknp6n52fpsj6ctln",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "sei1stwdtk6ja0705v8qmtukcp4vd422p5vy4jr5wdc4qk44c57k955qcannhd",
			},
		},
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei1stwdtk6ja0705v8qmtukcp4vd422p5vy4jr5wdc4qk44c57k955qcannhd",
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei1udfs22xpxle475m2nz7u47jfa3vngncdegmczwwdx00cmetypa3sman25q",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "sei14rse3e7rkc3qt7drmlulwlkrlzqvh7hv277zv05kyfuwl74udx5s9r7lm3",
			},
		},
		Suspended: true,
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei14rse3e7rkc3qt7drmlulwlkrlzqvh7hv277zv05kyfuwl74udx5s9r7lm3",
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: "sei182jzjwdyl5fw43yujnlljddgtrkr04dpd30ywp2yn724u7qhtaqsjev6mv",
	})

	require.Equal(t, []string{
		"sei1ery8l6jquynn9a4cz2pff6khg8c68f7urt33l5n9dng2cwzz4c4q4hncrd",
		"sei1wl59k23zngj34l7d42y9yltask7rjlnxgccawc7ltrknp6n52fpsj6ctln",
		"sei1stwdtk6ja0705v8qmtukcp4vd422p5vy4jr5wdc4qk44c57k955qcannhd",
	}, dex.GetAllDownstreamContracts(ctx, "sei1ery8l6jquynn9a4cz2pff6khg8c68f7urt33l5n9dng2cwzz4c4q4hncrd", keeper.GetContractWithoutGasCharge))
}
