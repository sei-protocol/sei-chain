package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestUpdateTickSize(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)

	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test updated tick size
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: TestContractA,
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NoError(t, err)

	storedTickSize, _ := keeper.GetTickSizeForPair(ctx, TestContractA, keepertest.TestPair)
	require.Equal(t, sdk.MustNewDecFromStr("0.1"), storedTickSize)
}

func TestUpdateTickSizeInvalidMsg(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)
	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test with empty creator address
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: TestContractA,
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      "",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with empty msg
	tickUpdates = []types.TickSize{}
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with invalid Creator address
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: TestContractA,
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      "invalidAddress",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with empty contract address
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: "",
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with nil pair
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: "",
		Pair:         nil,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)
}

// Test only contract creator can update tick size for contract
func TestInvalidUpdateTickSizeCreator(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)

	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test invalid tx creator
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: TestContractA,
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdateTickSize(wctx, &types.MsgUpdateTickSize{
		Creator:      "sei18rrckuelmacz4fv4v2hl9t3kaw7mm4wpe8v36m",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)
}
