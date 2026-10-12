package keeper_test

import (
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

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
	var denoms []string
	iterator := app.TokenFactoryKeeper.GetAllDenomsIterator(suite.Ctx)
	for ; iterator.Valid(); iterator.Next() {
		denoms = append(denoms, string(iterator.Value()))
	}
	suite.Require().NoError(iterator.Close())
	suite.Require().Len(denoms, len(genesisState.FactoryDenoms))
	for i, denom := range genesisState.FactoryDenoms {
		suite.Require().Equal(denom.Denom, denoms[i])
		metadata, err := app.TokenFactoryKeeper.GetAuthorityMetadata(suite.Ctx, denom.Denom)
		suite.Require().NoError(err)
		suite.Require().Equal(denom.AuthorityMetadata, metadata)
	}
	suite.Require().Equal(genesisState.Params, app.TokenFactoryKeeper.GetParams(suite.Ctx))
}
