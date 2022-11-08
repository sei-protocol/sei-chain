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
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: TestContractA,
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err := server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})

	require.NoError(t, err)
	storedRegisteredPairs := keeper.GetAllRegisteredPairs(ctx, TestContractA)
	require.Equal(t, 1, len(storedRegisteredPairs))
	require.Equal(t, keepertest.TestPair, storedRegisteredPairs[0])

	// Test multiple pairs registered at once
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
