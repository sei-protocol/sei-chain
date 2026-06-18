package keeper_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
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

func (suite *KeeperTestSuite) TestDenomsFromCreatorPagination() {
	creator := suite.TestAccs[0].String()
	ctx := sdk.WrapSDKContext(suite.Ctx)

	denomSubdirs := []string{"aaa", "bbb", "ccc", "ddd", "eee"}
	for _, sub := range denomSubdirs {
		_, err := suite.msgServer.CreateDenom(ctx, types.NewMsgCreateDenom(creator, sub))
		suite.Require().NoError(err)
	}

	suite.Run("no pagination returns all denoms", func() {
		res, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{Creator: creator})
		suite.Require().NoError(err)
		suite.Require().Len(res.Denoms, len(denomSubdirs))
		suite.Require().NotNil(res.Pagination)
	})

	suite.Run("limit 2 returns first page with next key", func() {
		res, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{
			Creator:    creator,
			Pagination: &query.PageRequest{Limit: 2},
		})
		suite.Require().NoError(err)
		suite.Require().Len(res.Denoms, 2)
		suite.Require().NotNil(res.Pagination)
		suite.Require().NotNil(res.Pagination.NextKey)
	})

	suite.Run("key-based second page returns remaining denoms", func() {
		first, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{
			Creator:    creator,
			Pagination: &query.PageRequest{Limit: 2},
		})
		suite.Require().NoError(err)

		second, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{
			Creator:    creator,
			Pagination: &query.PageRequest{Key: first.Pagination.NextKey, Limit: 2},
		})
		suite.Require().NoError(err)
		suite.Require().NotEmpty(second.Denoms)
		// pages must not overlap
		for _, d := range second.Denoms {
			suite.Require().NotContains(first.Denoms, d)
		}
	})

	suite.Run("offset-based pagination", func() {
		all, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{Creator: creator})
		suite.Require().NoError(err)

		const offset = 2
		res, err := suite.queryClient.DenomsFromCreator(ctx, &types.QueryDenomsFromCreatorRequest{
			Creator:    creator,
			Pagination: &query.PageRequest{Offset: offset, Limit: 2},
		})
		suite.Require().NoError(err)
		suite.Require().NotEmpty(res.Denoms)
		suite.Require().Equal(all.Denoms[offset:offset+len(res.Denoms)], res.Denoms)
	})
}

func TestKeeper_DenomAllowList(t *testing.T) {
	type args struct {
		req *types.QueryDenomAllowListRequest
		c   context.Context
	}
	tests := []struct {
		name    string
		args    args
		want    *types.QueryDenomAllowListResponse
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil request",
			args: args{
				req: nil,
				c:   context.Background(),
			},
			wantErr: true,
			errMsg:  "rpc error: code = InvalidArgument desc = empty request",
		},
		{
			name: "empty denom",
			args: args{
				req: &types.QueryDenomAllowListRequest{},
				c:   context.Background(),
			},
			wantErr: true,
			errMsg:  "rpc error: code = InvalidArgument desc = invalid denom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := keeper.Keeper{}
			got, err := k.DenomAllowList(tt.args.c, tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DenomAllowList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("DenomAllowList() error = %v, wantErr %v", err, tt.errMsg)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DenomAllowList() got = %v, want %v", got, tt.want)
			}
		})
	}
}
