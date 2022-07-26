package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterContract(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := dexkeeper.NewMsgServerImpl(*keeper, nil)
	contractInfo := types.ContractInfo{
		CodeId:       1,
		ContractAddr: TEST_CONTRACT,
	}
	server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  TEST_ACCOUNT,
		Contract: &contractInfo,
	})
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Nil(t, storedContracts[0].DependentContractAddrs)

	contractInfo.DependentContractAddrs = []string{"TEST2"}
	server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  TEST_ACCOUNT,
		Contract: &contractInfo,
	})
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Equal(t, 1, len(storedContracts[0].DependentContractAddrs))
}

func TestRegisterContractCircularDependency(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := dexkeeper.NewMsgServerImpl(*keeper, nil)
	contractInfo1 := types.ContractInfo{
		CodeId:                 1,
		ContractAddr:           "test1",
		DependentContractAddrs: []string{"test2"},
	}
	server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  TEST_ACCOUNT,
		Contract: &contractInfo1,
	})
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Equal(t, uint64(1), storedContracts[0].CodeId)

	// This contract should fail to be registered because it causes a
	// circular dependency
	contractInfo2 := types.ContractInfo{
		CodeId:                 2,
		ContractAddr:           "test2",
		DependentContractAddrs: []string{"test1"},
	}
	_, err := server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  TEST_ACCOUNT,
		Contract: &contractInfo2,
	})
	require.NotNil(t, err)
}
