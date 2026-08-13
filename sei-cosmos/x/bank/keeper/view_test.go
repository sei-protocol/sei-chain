package keeper_test

import (
	"encoding/hex"
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (suite *IntegrationTestSuite) TestViewKeeperStoreTrace() {
	app, ctx := suite.app, suite.ctx
	_, _, addr := testdata.KeyTestPubAddr()
	origCoins := sdk.NewCoins(newFooCoin(50), newBarCoin(30))
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr)

	app.AccountKeeper.SetAccount(ctx, acc)
	suite.Require().NoError(simapp.FundAccount(app.BankKeeper, ctx, acc.GetAddress(), origCoins))

	ctx = ctx.WithIsTracing(true)
	app.BankKeeper.GetBalance(ctx, addr, fooDenom)
	app.BankKeeper.GetAllBalances(ctx, addr)

	trace := ctx.StoreTracer().DerivePrestateToJson()
	typedTrace := &sdk.StoreTraceDump{}
	suite.Require().NoError(json.Unmarshal(trace, &typedTrace))
	suite.Require().Len(typedTrace.Modules, 1)
	suite.Require().Contains(typedTrace.Modules, "bank")

	bankDump := typedTrace.Modules["bank"]
	fooKey := append(banktypes.CreateAccountBalancesPrefix(addr), []byte(fooDenom)...)
	barKey := append(banktypes.CreateAccountBalancesPrefix(addr), []byte(barDenom)...)
	suite.Require().Len(bankDump.Has, 2)
	suite.Require().Contains(bankDump.Has, hex.EncodeToString(fooKey))
	suite.Require().Contains(bankDump.Has, hex.EncodeToString(barKey))
	suite.Require().Len(bankDump.Reads, 2)
	suite.Require().Contains(bankDump.Reads, hex.EncodeToString(fooKey))
	suite.Require().Contains(bankDump.Reads, hex.EncodeToString(barKey))
	fooCoin, barCoin := sdk.Coin{}, sdk.Coin{}
	fooBz, err := hex.DecodeString(bankDump.Reads[hex.EncodeToString(fooKey)])
	suite.Require().NoError(err)
	barBz, err := hex.DecodeString(bankDump.Reads[hex.EncodeToString(barKey)])
	suite.Require().NoError(err)
	suite.Require().NoError(app.BankKeeper.GetCdc().Unmarshal(fooBz, &fooCoin))
	suite.Require().NoError(app.BankKeeper.GetCdc().Unmarshal(barBz, &barCoin))
	suite.Require().Equal(sdk.NewInt(50), fooCoin.Amount)
	suite.Require().Equal(sdk.NewInt(30), barCoin.Amount)
}
