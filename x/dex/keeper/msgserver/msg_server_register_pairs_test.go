package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterPairs(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)

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
	storedRegisteredPairs := keeper.GetAllRegisteredPairs(ctx, TestContractA)
	require.Equal(t, 1, len(storedRegisteredPairs))
	require.Equal(t, keepertest.TestPair, storedRegisteredPairs[0])

	// Test multiple pairs registered at once
	err = RegisterContractUtil(server, wctx, TestContractB, nil)
	require.NoError(t, err)
	multiplePairs := []types.BatchContractPair{}
	secondTestPair := types.Pair{
		PriceDenom: "sei",
		AssetDenom: "osmo",
		Ticksize:   &keepertest.TestTicksize,
	}
	multiplePairs = append(multiplePairs, types.BatchContractPair{
		ContractAddr: TestContractB,
		Pairs:        []*types.Pair{&keepertest.TestPair, &secondTestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: multiplePairs,
	})

	require.NoError(t, err)
	storedRegisteredPairs = keeper.GetAllRegisteredPairs(ctx, TestContractB)
	require.Equal(t, 2, len(storedRegisteredPairs))
	require.Equal(t, keepertest.TestPair, storedRegisteredPairs[0])
	require.Equal(t, secondTestPair, storedRegisteredPairs[1])
}

func TestRegisterPairsInvalidMsg(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})

	// Test with empty creator address
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "",
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty msg
	batchContractPairs = []types.BatchContractPair{}
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with invalid Creator address
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "invalidAddress",
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty contract address
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: "",
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty pairs list
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with nil pair
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{nil},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)
}

// Test only contract creator can update registered pairs for contract
func TestInvalidRegisterPairCreator(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)

	// Expect error when registering pair with an address not contract creator
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "sei18rrckuelmacz4fv4v2hl9t3kaw7mm4wpe8v36m",
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Works when creator = address
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)
}
