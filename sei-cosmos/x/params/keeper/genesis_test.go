package keeper_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

type GenesisTestSuite struct {
	suite.Suite
	ctx    sdk.Context
	keeper keeper.Keeper
}

func (suite *GenesisTestSuite) SetupTest() {
	checkTx := false
	app := seiapp.Setup(suite.T(), checkTx, false, false)
	suite.ctx = app.BaseApp.NewContext(checkTx, tmproto.Header{Height: 1})
	suite.keeper = app.ParamsKeeper
}

func (suite *GenesisTestSuite) TestImportExportGenesis() {
	feesParams := &types.FeesParams{
		GlobalMinimumGasPrices: sdk.DecCoins{sdk.NewDecCoinFromDec(sdk.DefaultBondDenom, sdk.NewDecWithPrec(1, 3))},
	}
	cosmosGasParams := &types.CosmosGasParams{
		CosmosGasMultiplierNumerator:   1,
		CosmosGasMultiplierDenominator: 2,
	}

	suite.keeper.SetFeesParams(suite.ctx, *feesParams)
	suite.keeper.SetCosmosGasParams(suite.ctx, *cosmosGasParams)

	genesis := suite.keeper.ExportGenesis(suite.ctx)
	suite.Require().Equal(
		&types.GenesisState{
			FeesParams:      *feesParams,
			CosmosGasParams: *cosmosGasParams,
		},
		genesis,
	)
}

func (suite *GenesisTestSuite) TestInitGenesis() {
	validGenesis := &types.GenesisState{
		FeesParams: types.FeesParams{
			GlobalMinimumGasPrices: sdk.DecCoins{sdk.NewDecCoinFromDec(sdk.DefaultBondDenom, sdk.NewDecWithPrec(1, 1))},
		},
		CosmosGasParams: types.CosmosGasParams{
			CosmosGasMultiplierNumerator:   1,
			CosmosGasMultiplierDenominator: 4,
		},
	}
	suite.keeper.InitGenesis(suite.ctx, validGenesis)

	suite.Require().Equal(
		types.FeesParams{
			GlobalMinimumGasPrices: sdk.DecCoins{sdk.NewDecCoinFromDec(sdk.DefaultBondDenom, sdk.NewDecWithPrec(1, 1))},
		},
		suite.keeper.GetFeesParams(suite.ctx),
	)
	suite.Require().Equal(
		types.CosmosGasParams{
			CosmosGasMultiplierNumerator:   1,
			CosmosGasMultiplierDenominator: 4,
		},
		suite.keeper.GetCosmosGasParams(suite.ctx),
	)
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}
