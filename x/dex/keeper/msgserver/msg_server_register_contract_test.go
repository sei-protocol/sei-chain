package msgserver_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterContract(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := registerContract(server, wctx, keepertest.TestContract, nil)
	require.NoError(t, err)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Nil(t, storedContracts[0].Dependencies)

	// dependency doesn't exist
	err = registerContract(server, wctx, keepertest.TestContract, []string{"TEST2"})
	require.NotNil(t, err)
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
}

func TestRegisterContractCircularDependency(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	registerContract(server, wctx, "test1", nil)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))

	registerContract(server, wctx, "test2", []string{"test1"})
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 2, len(storedContracts))

	// This contract should fail to be registered because it causes a
	// circular dependency
	err := registerContract(server, wctx, "test1", []string{"test2"})
	require.NotNil(t, err)
}

func TestRegisterContractDuplicateDependency(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := registerContract(server, wctx, "test1", []string{"test2", "test2"})
	require.NotNil(t, err)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 0, len(storedContracts))
}

func TestRegisterContractNumIncomingPaths(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	registerContract(server, wctx, "test1", nil)
	storedContract, err := keeper.GetContract(ctx, "test1")
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	registerContract(server, wctx, "test2", []string{"test1"})
	storedContract, err = keeper.GetContract(ctx, "test1")
	require.Nil(t, err)
	require.Equal(t, int64(1), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, "test2")
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	registerContract(server, wctx, "test2", nil)
	storedContract, err = keeper.GetContract(ctx, "test1")
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, "test2")
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
}

func TestRegisterContractSetSiblings(t *testing.T) {
	// A -> X, B -> X, C -> Y
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	registerContract(server, wctx, "X", nil)
	registerContract(server, wctx, "Y", nil)
	registerContract(server, wctx, "A", []string{"X"})
	registerContract(server, wctx, "B", []string{"X"})
	registerContract(server, wctx, "C", []string{"Y"})
	// add D -> X, D -> Y
	registerContract(server, wctx, "D", []string{"X", "Y"})
	contract, _ := keeper.GetContract(ctx, "A")
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "B", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "B")
	require.Equal(t, "A", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "D", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "C")
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "D", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "D")
	require.Equal(t, "B", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	require.Equal(t, "C", contract.Dependencies[1].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[1].ImmediateYoungerSibling)
	// update D -> X only
	registerContract(server, wctx, "D", []string{"X"})
	contract, _ = keeper.GetContract(ctx, "A")
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "B", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "B")
	require.Equal(t, "A", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "D", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "C")
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, "D")
	require.Equal(t, 1, len(contract.Dependencies))
	require.Equal(t, "B", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
}

func registerContract(server types.MsgServer, ctx context.Context, contractAddr string, dependencies []string) error {
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddr,
	}
	if dependencies != nil {
		contract.Dependencies = utils.Map(dependencies, func(addr string) *types.ContractDependencyInfo {
			return &types.ContractDependencyInfo{
				Dependency: addr,
			}
		})
	}
	_, err := server.RegisterContract(ctx, &types.MsgRegisterContract{
		Creator:  keepertest.TestAccount,
		Contract: &contract,
	})
	return err
}
