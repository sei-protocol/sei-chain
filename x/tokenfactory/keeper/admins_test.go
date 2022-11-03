package keeper_test

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (suite *KeeperTestSuite) TestAdminMsgs() {
	addr0bal := int64(0)
	addr1bal := int64(0)

	suite.CreateDefaultDenom()
	// Make sure that the admin is set correctly
	queryRes, err := suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
		Denom: suite.defaultDenom,
	})
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(suite.TestAccs[0].String(), queryRes.AuthorityMetadata.Admin)

	// Test minting to admins own account
	_, err = suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), types.NewMsgMint(suite.TestAccs[0].String(), sdk.NewInt64Coin(suite.defaultDenom, 10)))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	addr0bal += 10
	suite.Require().NoError(err)
	suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom).Amount.Int64() == addr0bal, suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom))

	// // Test force transferring
	// _, err = suite.msgServer.ForceTransfer(sdk.WrapSDKContext(suite.Ctx), types.NewMsgForceTransfer(suite.TestAccs[0].String(), sdk.NewInt64Coin(denom, 5), suite.TestAccs[1].String(), suite.TestAccs[0].String()))
	// suite.Require().NoError(err)
	// suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], denom).IsEqual(sdk.NewInt64Coin(denom, 15)))
	// suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[1], denom).IsEqual(sdk.NewInt64Coin(denom, 5)))

	// Test burning from own account
	_, err = suite.msgServer.Burn(sdk.WrapSDKContext(suite.Ctx), types.NewMsgBurn(suite.TestAccs[0].String(), sdk.NewInt64Coin(suite.defaultDenom, 5)))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	addr0bal -= 5
	suite.Require().NoError(err)
	suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[1], suite.defaultDenom).Amount.Int64() == addr1bal)

	// Test Change Admin
	_, err = suite.msgServer.ChangeAdmin(sdk.WrapSDKContext(suite.Ctx), types.NewMsgChangeAdmin(suite.TestAccs[0].String(), suite.defaultDenom, suite.TestAccs[1].String()))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	queryRes, err = suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
		Denom: suite.defaultDenom,
	})
	suite.Require().NoError(err)
	suite.Require().Equal(suite.TestAccs[1].String(), queryRes.AuthorityMetadata.Admin)

	// Make sure old admin can no longer do actions
	_, err = suite.msgServer.Burn(sdk.WrapSDKContext(suite.Ctx), types.NewMsgBurn(suite.TestAccs[0].String(), sdk.NewInt64Coin(suite.defaultDenom, 5)))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	suite.Require().Error(err)

	// Make sure the new admin works
	_, err = suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), types.NewMsgMint(suite.TestAccs[1].String(), sdk.NewInt64Coin(suite.defaultDenom, 5)))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	addr1bal += 5
	suite.Require().NoError(err)
	suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[1], suite.defaultDenom).Amount.Int64() == addr1bal)

	// Try setting admin to empty
	_, err = suite.msgServer.ChangeAdmin(sdk.WrapSDKContext(suite.Ctx), types.NewMsgChangeAdmin(suite.TestAccs[1].String(), suite.defaultDenom, ""))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	suite.Require().NoError(err)
	queryRes, err = suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
		Denom: suite.defaultDenom,
	})
	suite.Require().NoError(err)
	suite.Require().Equal("", queryRes.AuthorityMetadata.Admin)
}

// TestMintDenom ensures the following properties of the MintMessage:
// * Noone can mint tokens for a denom that doesn't exist
// * Only the admin of a denom can mint tokens for it
// * The admin of a denom can mint tokens for it
func (suite *KeeperTestSuite) TestMintDenom() {
	var addr0bal int64

	// Create a denom
	suite.CreateDefaultDenom()

	for _, tc := range []struct {
		desc      string
		amount    int64
		mintDenom string
		admin     string
		valid     bool
	}{
		{
			desc:      "denom does not exist",
			amount:    10,
			mintDenom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/evmos",
			admin:     suite.TestAccs[0].String(),
			valid:     false,
		},
		{
			desc:      "mint is not by the admin",
			amount:    10,
			mintDenom: suite.defaultDenom,
			admin:     suite.TestAccs[1].String(),
			valid:     false,
		},
		{
			desc:      "success case",
			amount:    10,
			mintDenom: suite.defaultDenom,
			admin:     suite.TestAccs[0].String(),
			valid:     true,
		},
	} {
		suite.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			// Test minting to admins own account
			_, err := suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), types.NewMsgMint(tc.admin, sdk.NewInt64Coin(tc.mintDenom, 10)))
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

			if tc.valid {
				addr0bal += 10
				suite.Require().NoError(err)
				suite.Require().Equal(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom).Amount.Int64(), addr0bal, suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom))
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestBurnDenom() {
	var addr0bal int64

	// Create a denom.
	suite.CreateDefaultDenom()

	// mint 10 default token for testAcc[0]
	suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), types.NewMsgMint(suite.TestAccs[0].String(), sdk.NewInt64Coin(suite.defaultDenom, 10)))
	suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

	addr0bal += 10

	for _, tc := range []struct {
		desc      string
		amount    int64
		burnDenom string
		admin     string
		valid     bool
	}{
		{
			desc:      "denom does not exist",
			amount:    10,
			burnDenom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/evmos",
			admin:     suite.TestAccs[0].String(),
			valid:     false,
		},
		{
			desc:      "burn is not by the admin",
			amount:    10,
			burnDenom: suite.defaultDenom,
			admin:     suite.TestAccs[1].String(),
			valid:     false,
		},
		{
			desc:      "burn amount is bigger than minted amount",
			amount:    1000,
			burnDenom: suite.defaultDenom,
			admin:     suite.TestAccs[1].String(),
			valid:     false,
		},
		{
			desc:      "success case",
			amount:    10,
			burnDenom: suite.defaultDenom,
			admin:     suite.TestAccs[0].String(),
			valid:     true,
		},
	} {
		suite.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			// Test minting to admins own account
			_, err := suite.msgServer.Burn(sdk.WrapSDKContext(suite.Ctx), types.NewMsgBurn(tc.admin, sdk.NewInt64Coin(tc.burnDenom, 10)))
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

			if tc.valid {
				addr0bal -= 10
				suite.Require().NoError(err)
				suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom).Amount.Int64() == addr0bal, suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom))
			} else {
				suite.Require().Error(err)
				suite.Require().True(suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom).Amount.Int64() == addr0bal, suite.App.BankKeeper.GetBalance(suite.Ctx, suite.TestAccs[0], suite.defaultDenom))
			}
		})
	}
}

func (suite *KeeperTestSuite) TestChangeAdminDenom() {
	for _, tc := range []struct {
		desc                    string
		msgChangeAdmin          func(denom string) *types.MsgChangeAdmin
		expectedChangeAdminPass bool
		expectedAdminIndex      int
		msgMint                 func(denom string) *types.MsgMint
		expectedMintPass        bool
	}{
		{
			desc: "creator admin can't mint after setting to '' ",
			msgChangeAdmin: func(denom string) *types.MsgChangeAdmin {
				return types.NewMsgChangeAdmin(suite.TestAccs[0].String(), denom, "")
			},
			expectedChangeAdminPass: true,
			expectedAdminIndex:      -1,
			msgMint: func(denom string) *types.MsgMint {
				return types.NewMsgMint(suite.TestAccs[0].String(), sdk.NewInt64Coin(denom, 5))
			},
			expectedMintPass: false,
		},
		{
			desc: "non-admins can't change the existing admin",
			msgChangeAdmin: func(denom string) *types.MsgChangeAdmin {
				return types.NewMsgChangeAdmin(suite.TestAccs[1].String(), denom, suite.TestAccs[2].String())
			},
			expectedChangeAdminPass: false,
			expectedAdminIndex:      0,
		},
		{
			desc: "success change admin",
			msgChangeAdmin: func(denom string) *types.MsgChangeAdmin {
				return types.NewMsgChangeAdmin(suite.TestAccs[0].String(), denom, suite.TestAccs[1].String())
			},
			expectedAdminIndex:      1,
			expectedChangeAdminPass: true,
			msgMint: func(denom string) *types.MsgMint {
				return types.NewMsgMint(suite.TestAccs[1].String(), sdk.NewInt64Coin(denom, 5))
			},
			expectedMintPass: true,
		},
	} {
		suite.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			// setup test
			suite.SetupTest()

			// Create a denom and mint
			res, err := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)
			suite.Require().NoError(err)

			testDenom := res.GetNewTokenDenom()

			_, err = suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), types.NewMsgMint(suite.TestAccs[0].String(), sdk.NewInt64Coin(testDenom, 10)))
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

			suite.Require().NoError(err)

			_, err = suite.msgServer.ChangeAdmin(sdk.WrapSDKContext(suite.Ctx), tc.msgChangeAdmin(testDenom))
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

			if tc.expectedChangeAdminPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

			queryRes, err := suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
				Denom: testDenom,
			})
			suite.Require().NoError(err)

			// expectedAdminIndex with negative value is assumed as admin with value of ""
			const emptyStringAdminIndexFlag = -1
			if tc.expectedAdminIndex == emptyStringAdminIndexFlag {
				suite.Require().Equal("", queryRes.AuthorityMetadata.Admin)
			} else {
				suite.Require().Equal(suite.TestAccs[tc.expectedAdminIndex].String(), queryRes.AuthorityMetadata.Admin)
			}

			// we test mint to test if admin authority is performed properly after admin change.
			if tc.msgMint != nil {
				_, err := suite.msgServer.Mint(sdk.WrapSDKContext(suite.Ctx), tc.msgMint(testDenom))
				suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

				if tc.expectedMintPass {
					suite.Require().NoError(err)
				} else {
					suite.Require().Error(err)
				}
			}
		})
	}
}
