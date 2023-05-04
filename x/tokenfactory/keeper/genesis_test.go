package keeper_test

import (
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (suite *KeeperTestSuite) TestGenesis() {
	genesisState := types.GenesisState{
		FactoryDenoms: []types.GenesisDenom{
			{
				Denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/bitcoin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
				},
			},
			{
				Denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/diff-admin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei1hjfwcza3e3uzeznf3qthhakdr9juetl7g6esl4",
				},
			},
			{
				Denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/litecoin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
				},
			},
		},
	}
	app := suite.App
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})
	// Test both with bank denom metadata set, and not set.
	for i, denom := range genesisState.FactoryDenoms {
		// hacky, sets bank metadata to exist if i != 0, to cover both cases.
		if i != 0 {
			app.BankKeeper.SetDenomMetaData(suite.Ctx, banktypes.Metadata{Base: denom.GetDenom()})
		}
	}

	app.TokenFactoryKeeper.InitGenesis(suite.Ctx, genesisState)
	exportedGenesis := app.TokenFactoryKeeper.ExportGenesis(suite.Ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(genesisState, *exportedGenesis)
}
