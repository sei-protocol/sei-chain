package keeper_test

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (suite *KeeperTestSuite) TestDenomMetadataRequest() {
	var (
		req         *types.QueryDenomMetadataRequest
		expMetadata = banktypes.Metadata{}
	)
	tokenFactoryDenom := "factory/sei1gxskuzvhr4s8sdm2rpruaf7yx2dnmjn0zfdu9q/NEWCOIN"
	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"empty denom",
			func() {
				req = &types.QueryDenomMetadataRequest{}
			},
			false,
		},
		{
			"not found denom",
			func() {
				req = &types.QueryDenomMetadataRequest{
					Denom: tokenFactoryDenom,
				}
			},
			false,
		},
		{
			"success",
			func() {

				expMetadata = banktypes.Metadata{
					Description: "Token factory custom token",
					DenomUnits: []*banktypes.DenomUnit{
						{
							Denom:    tokenFactoryDenom,
							Exponent: 0,
							Aliases:  []string{tokenFactoryDenom},
						},
					},
					Base:    tokenFactoryDenom,
					Display: tokenFactoryDenom,
				}

				suite.App.BankKeeper.SetDenomMetaData(suite.Ctx, expMetadata)
				req = &types.QueryDenomMetadataRequest{
					Denom: expMetadata.Base,
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.Ctx)

			res, err := suite.queryClient.DenomMetadata(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expMetadata, res.Metadata)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestDenomAllowListRequest() {

	tokenFactoryDenom := "factory/sei1gxskuzvhr4s8sdm2rpruaf7yx2dnmjn0zfdu9q/NEWCOIN"
	allowList := banktypes.AllowList{
		Addresses: []string{"sei1gxskuzvhr4s8sdm2rpruaf7yx2dnmjn0zfdu9q", "sei1gxskuzvhr4s8sdm2rpruaf7yx2dnmjn0zfdu8q"},
	}
	type args struct {
		req *types.QueryDenomAllowListRequest
	}
	testCases := []struct {
		name          string
		args          args
		malleate      func()
		expAllowList  banktypes.AllowList
		expectedError string
		wantErr       bool
	}{
		{
			name:     "fails on empty denom",
			malleate: func() {},
			args: args{
				req: &types.QueryDenomAllowListRequest{},
			},
			expectedError: "rpc error: code = InvalidArgument desc = invalid denom",
			wantErr:       true,
		},
		{
			name:     "returns empty list for denom that does not have allow list",
			malleate: func() {},
			args: args{
				req: &types.QueryDenomAllowListRequest{
					Denom: tokenFactoryDenom,
				},
			},
			expAllowList: banktypes.AllowList{},
			wantErr:      false,
		},
		{
			name: "returns allow list for denom that has allow list",
			malleate: func() {
				suite.App.BankKeeper.SetDenomAllowList(suite.Ctx, tokenFactoryDenom, allowList)
			},
			args: args{
				req: &types.QueryDenomAllowListRequest{
					Denom: tokenFactoryDenom,
				},
			},
			expAllowList: allowList,
			wantErr:      false,
		},
	}

	for _, tc := range testCases {

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.Ctx)

			res, err := suite.queryClient.DenomAllowList(ctx, tc.args.req)

			if tc.wantErr {
				suite.Require().Error(err)
				suite.Require().Contains(err.Error(), tc.expectedError)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(tc.expAllowList, res.AllowList)
			}
		})
	}
}
