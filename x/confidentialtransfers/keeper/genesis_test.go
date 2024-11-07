package keeper_test

import (
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func (suite *KeeperTestSuite) TestDefaultGenesisState() {
	genesisState := types.DefaultGenesisState()

	app := suite.App
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})

	suite.App.ConfidentialTransfersKeeper.InitGenesis(suite.Ctx, genesisState)
	exportedGenesis := suite.App.ConfidentialTransfersKeeper.ExportGenesis(suite.Ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(genesisState, exportedGenesis)
}
