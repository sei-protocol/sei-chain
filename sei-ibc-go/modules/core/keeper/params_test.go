package keeper_test

import (
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

func (suite *KeeperTestSuite) TestCoreParams_GetSet() {
	ctx := suite.chainA.GetContext()
	ik := suite.chainA.App.GetIBCKeeper()

	// default params should be true,true
	params := ik.GetParams(ctx)
	suite.Require().True(params.InboundEnabled)
	suite.Require().True(params.OutboundEnabled)

	// toggle inbound -> false
	ik.SetInboundEnabled(ctx, false)
	suite.Require().False(ik.IsInboundEnabled(ctx))

	// toggle outbound -> false
	ik.SetOutboundEnabled(ctx, false)
	suite.Require().False(ik.IsOutboundEnabled(ctx))

	// restore defaults
	ik.SetParams(ctx, types.DefaultParams())
	params = ik.GetParams(ctx)
	suite.Require().True(params.InboundEnabled)
	suite.Require().True(params.OutboundEnabled)
}

