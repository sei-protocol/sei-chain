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

const (
	TestContractA = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	TestContractB = "sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a"
	TestContractC = "sei1xr3rq8yvd7qplsw5yx90ftsr2zdhg4e9z60h5duusgxpv72hud3shh3qfl"
	TestContractD = "sei1up07dctjqud4fns75cnpejr4frmjtddzsmwgcktlyxd4zekhwecqghxqcp"
	TestContractX = "sei1hw5n2l4v5vz8lk4sj69j7pwdaut0kkn90mw09snlkdd3f7ckld0smdtvee"
	TestContractY = "sei12pwnhtv7yat2s30xuf4gdk9qm85v4j3e6p44let47pdffpklcxlqh8ag0z"
)

func TestRegisterContract(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, nil)
	require.NoError(t, err)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Nil(t, storedContracts[0].Dependencies)

	// dependency doesn't exist
	err = RegisterContractUtil(server, wctx, TestContractA, []string{TestContractY})
	require.NotNil(t, err)
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
}

func TestRegisterContractCircularDependency(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	RegisterContractUtil(server, wctx, TestContractA, nil)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))

	RegisterContractUtil(server, wctx, TestContractB, []string{TestContractA})
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 2, len(storedContracts))

	// This contract should fail to be registered because it causes a
	// circular dependency
	err := RegisterContractUtil(server, wctx, TestContractA, []string{TestContractA})
	require.NotNil(t, err)
}

func TestRegisterContractDuplicateDependency(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	err := RegisterContractUtil(server, wctx, TestContractA, []string{TestContractA, TestContractA})
	require.NotNil(t, err)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 0, len(storedContracts))
}

func TestRegisterContractNumIncomingPaths(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	RegisterContractUtil(server, wctx, TestContractA, nil)
	storedContract, err := keeper.GetContract(ctx, TestContractA)
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	RegisterContractUtil(server, wctx, TestContractB, []string{TestContractA})
	storedContract, err = keeper.GetContract(ctx, TestContractA)
	require.Nil(t, err)
	require.Equal(t, int64(1), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, TestContractB)
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	RegisterContractUtil(server, wctx, TestContractB, nil)
	storedContract, err = keeper.GetContract(ctx, TestContractA)
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, TestContractA)
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
}

func TestRegisterContractSetSiblings(t *testing.T) {
	// A -> X, B -> X, C -> Y
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	RegisterContractUtil(server, wctx, TestContractX, nil)
	RegisterContractUtil(server, wctx, TestContractY, nil)
	RegisterContractUtil(server, wctx, TestContractA, []string{TestContractX})
	RegisterContractUtil(server, wctx, TestContractB, []string{TestContractX})
	RegisterContractUtil(server, wctx, TestContractC, []string{TestContractY})
	// add D -> X, D -> Y
	RegisterContractUtil(server, wctx, TestContractD, []string{TestContractX, TestContractY})
	contract, _ := keeper.GetContract(ctx, TestContractA)
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, TestContractB, contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractB)
	require.Equal(t, TestContractA, contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, TestContractD, contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractC)
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, TestContractD, contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractD)
	require.Equal(t, TestContractB, contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	require.Equal(t, TestContractC, contract.Dependencies[1].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[1].ImmediateYoungerSibling)
	// update D -> X only
	RegisterContractUtil(server, wctx, TestContractD, []string{TestContractX})
	contract, _ = keeper.GetContract(ctx, TestContractA)
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, TestContractB, contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractB)
	require.Equal(t, TestContractA, contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, TestContractD, contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractC)
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, TestContractD)
	require.Equal(t, 1, len(contract.Dependencies))
	require.Equal(t, TestContractB, contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
}

func RegisterContractUtil(server types.MsgServer, ctx context.Context, contractAddr string, dependencies []string) error {
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
