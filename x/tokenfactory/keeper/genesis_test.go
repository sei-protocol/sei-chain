package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (suite *KeeperTestSuite) TestGenesis() {
	genesisState := types.GenesisState{
		FactoryDenoms: []types.GenesisDenom{
			{
				Denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/bitcoin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44",
				},
			},
			{
				Denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/diff-admin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei15czt5nhlnvayqq37xun9s9yus0d6y26dw9xnzn",
				},
			},
			{
				Denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/litecoin",
				AuthorityMetadata: types.DenomAuthorityMetadata{
					Admin: "sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44",
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

	app.TokenFactoryKeeper.SetParams(suite.Ctx, types.Params{DenomCreationFee: sdk.Coins{sdk.NewInt64Coin("usei", 100)}})
	app.TokenFactoryKeeper.InitGenesis(suite.Ctx, genesisState)
	exportedGenesis := app.TokenFactoryKeeper.ExportGenesis(suite.Ctx)
	suite.Require().NotNil(exportedGenesis)
	suite.Require().Equal(genesisState, *exportedGenesis)
}
