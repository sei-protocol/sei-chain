package keeper_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
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

// TestInitGenesisStoresTheDisabledMsgTypeURLs initializes a params store that does not hold
// the disabled message type URLs.
func TestInitGenesisStoresTheDisabledMsgTypeURLs(t *testing.T) {
	_, ctx, _, _, k := testComponents()
	subspace, ok := k.GetSubspace(types.ModuleName)
	require.True(t, ok)

	k.InitGenesis(ctx, types.DefaultGenesis())

	require.True(t, subspace.Has(ctx, types.ParamStoreKeyDisabledMsgTypeURLs))
	require.Empty(t, k.GetDisabledMsgTypeURLs(ctx))
}
