package keeper_test

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

func (suite *KeeperTestSuite) TestMigrate2to3() {
	ctx := suite.chainA.GetContext()
	ibcKeeper := suite.chainA.App.GetIBCKeeper()

	paramStore := prefix.NewStore(
		ctx.KVStore(suite.chainA.GetSimApp().GetKey(paramtypes.StoreKey)),
		[]byte(ibcKeeper.GetParamSpace().Name()+"/"),
	)
	paramStore.Delete(types.KeyInboundEnabled)
	paramStore.Delete(types.KeyOutboundEnabled)

	suite.Require().Panics(func() {
		ibcKeeper.GetParams(ctx)
	})

	m := ibckeeper.NewMigrator(*ibcKeeper)
	suite.Require().NoError(m.Migrate2to3(ctx))
	suite.Require().Equal(types.DefaultParams(), ibcKeeper.GetParams(ctx))
}
