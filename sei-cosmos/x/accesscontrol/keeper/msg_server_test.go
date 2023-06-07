package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func (suite *KeeperTestSuite) TestMessageRegisterWasmDependency() {
	suite.SetupTest()
	app := suite.app
	ctx := suite.ctx
	req := suite.Require()

	msgServer := keeper.NewMsgServerImpl(app.AccessControlKeeper)

	contractAddr := suite.addrs[0]
	fromAddr := suite.addrs[1]

	invalidRegisterWasmDependency := acltypes.MsgRegisterWasmDependency{
		FromAddress:           "Invalid",
		WasmDependencyMapping: acltypes.SynchronousWasmDependencyMapping(contractAddr.String()),
	}
	invalidRegisterWasmDependency.WasmDependencyMapping.BaseAccessOps = []*accesscontrol.WasmAccessOperation{}

	_, err := msgServer.RegisterWasmDependency(sdk.WrapSDKContext(ctx), &invalidRegisterWasmDependency)
	req.Error(err)

	validRegisterWasmDependency := acltypes.MsgRegisterWasmDependency{
		FromAddress:           fromAddr.String(),
		WasmDependencyMapping: acltypes.SynchronousWasmDependencyMapping(contractAddr.String()),
	}
	resp, err := msgServer.RegisterWasmDependency(sdk.WrapSDKContext(ctx), &validRegisterWasmDependency)
	req.NoError(err)
	req.Equal(acltypes.MsgRegisterWasmDependencyResponse{}, *resp)

	deps, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, contractAddr)
	req.NoError(err)
	req.Equal(acltypes.SynchronousWasmDependencyMapping(contractAddr.String()), *deps)
}

func (suite *KeeperTestSuite) TestMessageRegisterWasmDepFromJson() {
	suite.SetupTest()
	app := suite.app
	ctx := suite.ctx
	req := suite.Require()

	msgServer := keeper.NewMsgServerImpl(app.AccessControlKeeper)

	contractAddr := suite.addrs[0]
	fromAddr := suite.addrs[1]

	depJson := acltypes.RegisterWasmDependencyJSONFile{
		WasmDependencyMapping: acltypes.SynchronousWasmDependencyMapping(contractAddr.String()),
	}

	registerWasmDependency := acltypes.NewMsgRegisterWasmDependencyFromJSON(fromAddr, depJson)

	resp, err := msgServer.RegisterWasmDependency(sdk.WrapSDKContext(ctx), registerWasmDependency)

	req.Equal(acltypes.MsgRegisterWasmDependencyResponse{}, *resp)
	req.NoError(err)

	deps, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, contractAddr)
	req.NoError(err)
	req.Equal(acltypes.SynchronousWasmDependencyMapping(contractAddr.String()), *deps)
}
