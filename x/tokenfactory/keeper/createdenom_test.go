package keeper_test

import (
	"fmt"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (suite *KeeperTestSuite) TestMsgCreateDenom() {

	// Creating a denom should work
	res, err := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
	suite.Require().NoError(err)
	suite.Require().NotEmpty(res.GetNewTokenDenom())

	// Make sure that the admin is set correctly
	queryRes, err := suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
		Denom: res.GetNewTokenDenom(),
	})
	suite.Require().NoError(err)
	suite.Require().Equal(suite.TestAccs[0].String(), queryRes.AuthorityMetadata.Admin)

	// Make sure that a second version of the same denom can't be recreated
	res, err = suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
	suite.Require().Error(err)

	// Creating a second denom should work
	res, err = suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "litecoin"))
	suite.Require().NoError(err)
	suite.Require().NotEmpty(res.GetNewTokenDenom())

	// Try querying all the denoms created by suite.TestAccs[0]
	queryRes2, err := suite.queryClient.DenomsFromCreator(suite.Ctx.Context(), &types.QueryDenomsFromCreatorRequest{
		Creator: suite.TestAccs[0].String(),
	})
	suite.Require().NoError(err)
	suite.Require().Len(queryRes2.Denoms, 2)

	// Make sure that a second account can create a denom with the same subdenom
	res, err = suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[1].String(), "bitcoin"))
	suite.Require().NoError(err)
	suite.Require().NotEmpty(res.GetNewTokenDenom())

	// Make sure that an address with a "/" in it can't create denoms
	res, err = suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom("sei.eth/creator", "bitcoin"))
	suite.Require().Error(err)
}

func (suite *KeeperTestSuite) TestCreateDenom() {
	for _, tc := range []struct {
		desc      string
		setup     func()
		subdenom  string
		allowList *banktypes.AllowList
		valid     bool
	}{
		{
			desc:     "subdenom too long",
			subdenom: "assadsadsadasdasdsadsadsadsadsadsadsklkadaskkkdasdasedskhanhassyeunganassfnlksdflksafjlkasd",
			valid:    false,
		},
		{
			desc: "subdenom and creator pair already exists",
			setup: func() {
				_, err := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
				suite.Require().NoError(err)
			},
			subdenom: "bitcoin",
			valid:    false,
		},
		{
			desc:     "success case",
			subdenom: "evmos",
			valid:    true,
		},
		{
			desc:     "subdenom having invalid characters",
			subdenom: "bit/***///&&&/coin",
			valid:    false,
		},
		{
			desc:     "valid allow list",
			subdenom: "withallowlist",
			allowList: &banktypes.AllowList{
				Addresses: []string{suite.TestAccs[0].String(), suite.TestAccs[1].String()},
			},
			valid: true,
		},
		{
			desc:     "invalid allow list with invalid address",
			subdenom: "invalidallowlist",
			allowList: &banktypes.AllowList{
				Addresses: []string{"invalid_address"},
			},
			valid: false,
		},
	} {
		suite.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			if tc.setup != nil {
				tc.setup()
			}

			msg := types.NewMsgCreateDenom(suite.TestAccs[0].String(), tc.subdenom)
			if tc.allowList != nil {
				msg.AllowList = tc.allowList
			}

			// Create a denom
			res, err := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), msg)
			if tc.valid {
				suite.Require().NoError(err)

				// Make sure that the admin is set correctly
				queryRes, err := suite.queryClient.DenomAuthorityMetadata(suite.Ctx.Context(), &types.QueryDenomAuthorityMetadataRequest{
					Denom: res.GetNewTokenDenom(),
				})

				suite.Require().NoError(err)
				suite.Require().Equal(suite.TestAccs[0].String(), queryRes.AuthorityMetadata.Admin)

				// Make sure that the denom is valid from the perspective of x/bank
				bankQueryRes, err := suite.bankQueryClient.DenomMetadata(suite.Ctx.Context(), &banktypes.QueryDenomMetadataRequest{
					Denom: res.GetNewTokenDenom(),
				})

				suite.Require().NoError(err)
				suite.Require().NoError(bankQueryRes.Metadata.Validate())

				// Verify the allow list if provided
				if tc.allowList != nil {
					allowListRes, err := suite.queryClient.DenomAllowList(suite.Ctx.Context(), &types.QueryDenomAllowListRequest{
						Denom: res.GetNewTokenDenom(),
					})
					suite.Require().NoError(err)
					suite.Require().Equal(tc.allowList, allowListRes.AllowList)
				}
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
