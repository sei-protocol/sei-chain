package vesting_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

type HandlerTestSuite struct {
	suite.Suite

	handler sdk.Handler
	app     *app.App
}

func (suite *HandlerTestSuite) SetupTest() {
	checkTx := false
	app := app.Setup(suite.T(), checkTx, false, false)

	suite.handler = vesting.NewHandler(app.AccountKeeper, app.BankKeeper)
	suite.app = app
}

func (suite *HandlerTestSuite) TestMsgCreateVestingAccount() {
	ctx := suite.app.BaseApp.NewContext(false, tmproto.Header{Height: suite.app.LastBlockHeight() + 1})

	balances := sdk.NewCoins(sdk.NewInt64Coin("test", 1000))
	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	addr3 := sdk.AccAddress([]byte("addr3_______________"))
	addr4 := sdk.AccAddress([]byte("addr4_______________"))
	addr5 := sdk.AccAddress([]byte("addr5_______________"))
	addr6 := sdk.AccAddress([]byte("addr6_______________"))
	addr7 := sdk.AccAddress([]byte("addr7_______________"))
	addr8 := sdk.AccAddress([]byte("addr8_______________"))

	acc1 := suite.app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	suite.app.AccountKeeper.SetAccount(ctx, acc1)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, addr1, balances))
	acc4 := suite.app.AccountKeeper.NewAccountWithAddress(ctx, addr4)
	suite.app.AccountKeeper.SetAccount(ctx, acc4)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, addr4, balances))

	testCases := []struct {
		name      string
		msg       *types.MsgCreateVestingAccount
		expectErr bool
	}{
		{
			name:      "create delayed vesting account",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, nil),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr3, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, nil),
			expectErr: false,
		},
		{
			name:      "create delayed vesting account with admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr5, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, addr4),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account with admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr6, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, addr4),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account with non-existing admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr7, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, addr8),
			expectErr: true,
		},
		{
			name:      "continuous vesting account already exists",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr3, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, nil),
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			res, err := suite.handler(ctx, tc.msg)
			if tc.expectErr {
				suite.Require().Error(err)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				toAddr, err := sdk.AccAddressFromBech32(tc.msg.ToAddress)
				suite.Require().NoError(err)
				accI := suite.app.AccountKeeper.GetAccount(ctx, toAddr)
				suite.Require().NotNil(accI)

				if tc.msg.Delayed {
					acc, ok := accI.(*types.DelayedVestingAccount)
					suite.Require().True(ok)
					suite.Require().Equal(tc.msg.Amount, acc.GetVestingCoins(ctx.BlockTime()))
				} else {
					acc, ok := accI.(*types.ContinuousVestingAccount)
					suite.Require().True(ok)
					suite.Require().Equal(tc.msg.Amount, acc.GetVestingCoins(ctx.BlockTime()))
				}
			}
		})
	}
}

func TestHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}
