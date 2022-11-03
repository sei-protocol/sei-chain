package dex_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

const (
	TEST_CONTRACT = "test"
	TEST_PAIR     = "pair"
)

func TestDeepCopy(t *testing.T) {
	ctx := sdk.Context{}
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateTwo := stateOne.DeepCopy()
	stateTwo.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           2,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	// old state must not be changed
	require.Equal(t, 1, len(stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
	// new state must be changed
	require.Equal(t, 2, len(stateTwo.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
}

func TestDeepFilterAccounts(t *testing.T) {
	ctx := sdk.Context{}
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           2,
		Account:      "test2",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Cancellation{
		Id:      1,
		Creator: "test",
	})
	stateOne.GetBlockCancels(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Cancellation{
		Id:      2,
		Creator: "test2",
	})
	stateOne.GetDepositInfo(ctx, utils.ContractAddress(TEST_CONTRACT)).Add(&dex.DepositInfoEntry{
		Creator: "test",
	})
	stateOne.GetDepositInfo(ctx, utils.ContractAddress(TEST_CONTRACT)).Add(&dex.DepositInfoEntry{
		Creator: "test2",
	})

	stateOne.DeepFilterAccount("test")
	require.Equal(t, 1, stateOne.GetAllBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT)).Len())
	require.Equal(t, 1, len(stateOne.GetBlockCancels(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
	require.Equal(t, 1, len(stateOne.GetDepositInfo(ctx, utils.ContractAddress(TEST_CONTRACT)).Get()))
}

func TestClear(t *testing.T) {
	ctx := sdk.Context{}
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.Clear()
	require.Equal(t, 0, len(stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
}

func TestSynchronization(t *testing.T) {
	ctx := sdk.Context{}
	stateOne := dex.NewMemState()
	targetContract := utils.ContractAddress(TEST_CONTRACT)
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
