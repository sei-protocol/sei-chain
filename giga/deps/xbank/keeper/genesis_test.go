package keeper_test

import (
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func (suite *IntegrationTestSuite) getTestBalancesAndSupply() ([]types.Balance, sdk.Coins) {
	addr1, _ := sdk.AccAddressFromBech32("sei10xwrnrezdg227cgt82az7f7j47q3zklvu5ax6k")
	addr2, _ := sdk.AccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl")
	addr1Balance := sdk.Coins{sdk.NewInt64Coin("testcoin3", 10)}
	addr2Balance := sdk.Coins{sdk.NewInt64Coin("testcoin1", 32), sdk.NewInt64Coin("testcoin2", 34), sdk.NewInt64Coin(sdk.DefaultBondDenom, 2)}

	totalSupply := addr1Balance
	totalSupply = totalSupply.Add(addr2Balance...)

	return []types.Balance{
		{Address: addr2.String(), Coins: addr2Balance},
		{Address: addr1.String(), Coins: addr1Balance},
	}, totalSupply
}

func (suite *IntegrationTestSuite) TestInitGenesis() {
	m := types.Metadata{Description: sdk.DefaultBondDenom, Base: sdk.DefaultBondDenom, Display: sdk.DefaultBondDenom}
	g := types.DefaultGenesisState()
	g.DenomMetadata = []types.Metadata{m}
	bk := suite.app.GigaBankKeeper
	bk.InitGenesis(suite.ctx, g)

	m2, found := bk.GetDenomMetaData(suite.ctx, m.Base)
	suite.Require().True(found)
	suite.Require().Equal(m, m2)
}
