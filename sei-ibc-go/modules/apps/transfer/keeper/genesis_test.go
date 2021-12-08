package keeper_test

import (
	"fmt"

	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
)

func (suite *KeeperTestSuite) TestGenesis() {
	var (
		path   string
		traces types.Traces
	)

	for i := 0; i < 5; i++ {
		prefix := fmt.Sprintf("transfer/channelToChain%d", i)
		if i == 0 {
			path = prefix
		} else {
			path = prefix + "/" + path
		}

		denomTrace := types.DenomTrace{
			BaseDenom: "uatom",
			Path:      path,
		}
		traces = append(types.Traces{denomTrace}, traces...)
		suite.chainA.GetSimApp().TransferKeeper.SetDenomTrace(suite.chainA.GetContext(), denomTrace)
	}

	genesis := suite.chainA.GetSimApp().TransferKeeper.ExportGenesis(suite.chainA.GetContext())

	suite.Require().Equal(types.PortID, genesis.PortId)
	suite.Require().Equal(traces.Sort(), genesis.DenomTraces)

	suite.Require().NotPanics(func() {
		suite.chainA.GetSimApp().TransferKeeper.InitGenesis(suite.chainA.GetContext(), *genesis)
	})
}
