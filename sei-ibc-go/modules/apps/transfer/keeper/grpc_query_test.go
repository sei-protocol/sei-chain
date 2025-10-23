package keeper_test

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *KeeperTestSuite) TestQueryDenomTrace() {
	var (
		req      *types.QueryDenomTraceRequest
		expTrace types.DenomTrace
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"success: correct ibc denom",
			func() {
				expTrace.Path = "transfer/channelToA/transfer/channelToB"
				expTrace.BaseDenom = "uatom"
				suite.chainA.GetSimApp().TransferKeeper.SetDenomTrace(suite.chainA.GetContext(), expTrace)

				req = &types.QueryDenomTraceRequest{
					Hash: expTrace.IBCDenom(),
				}
			},
			true,
		},
		{
			"success: correct hex hash",
			func() {
				expTrace.Path = "transfer/channelToA/transfer/channelToB"
				expTrace.BaseDenom = "uatom"
				suite.chainA.GetSimApp().TransferKeeper.SetDenomTrace(suite.chainA.GetContext(), expTrace)

				req = &types.QueryDenomTraceRequest{
					Hash: expTrace.Hash().String(),
				}
			},
			true,
		},
		{
			"failure: invalid hash",
			func() {
				req = &types.QueryDenomTraceRequest{
					Hash: "!@#!@#!",
				}
			},
			false,
		},
		{
			"failure: not found denom trace",
			func() {
				expTrace.Path = "transfer/channelToA/transfer/channelToB"
				expTrace.BaseDenom = "uatom"
				req = &types.QueryDenomTraceRequest{
					Hash: expTrace.IBCDenom(),
				}
			},
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.chainA.GetContext())

			res, err := suite.queryClient.DenomTrace(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(&expTrace, res.DenomTrace)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryDenomTraces() {
	var (
		req       *types.QueryDenomTracesRequest
		expTraces = types.Traces(nil)
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"empty pagination",
			func() {
				req = &types.QueryDenomTracesRequest{}
			},
			true,
		},
		{
			"success",
			func() {
				expTraces = append(expTraces, types.DenomTrace{Path: "", BaseDenom: "uatom"})
				expTraces = append(expTraces, types.DenomTrace{Path: "transfer/channelToB", BaseDenom: "uatom"})
				expTraces = append(expTraces, types.DenomTrace{Path: "transfer/channelToA/transfer/channelToB", BaseDenom: "uatom"})

				for _, trace := range expTraces {
					suite.chainA.GetSimApp().TransferKeeper.SetDenomTrace(suite.chainA.GetContext(), trace)
				}

				req = &types.QueryDenomTracesRequest{
					Pagination: &query.PageRequest{
						Limit:      5,
						CountTotal: false,
					},
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.chainA.GetContext())

			res, err := suite.queryClient.DenomTraces(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expTraces.Sort(), res.DenomTraces)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryParams() {
	ctx := sdk.WrapSDKContext(suite.chainA.GetContext())
	expParams := types.DefaultParams()
	res, _ := suite.queryClient.Params(ctx, &types.QueryParamsRequest{})
	suite.Require().Equal(&expParams, res.Params)
}

func (suite *KeeperTestSuite) TestQueryDenomHash() {
	reqTrace := types.DenomTrace{
		Path:      "transfer/channelToA/transfer/channelToB",
		BaseDenom: "uatom",
	}

	var (
		req     *types.QueryDenomHashRequest
		expHash = reqTrace.Hash().String()
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"invalid trace",
			func() {
				req = &types.QueryDenomHashRequest{
					Trace: "transfer/channelToA/transfer/",
				}
			},
			false,
		},
		{
			"not found denom trace",
			func() {
				req = &types.QueryDenomHashRequest{
					Trace: "transfer/channelToC/uatom",
				}
			},
			false,
		},
		{
			"success",
			func() {},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			req = &types.QueryDenomHashRequest{
				Trace: reqTrace.GetFullDenomPath(),
			}
			suite.chainA.GetSimApp().TransferKeeper.SetDenomTrace(suite.chainA.GetContext(), reqTrace)

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.chainA.GetContext())

			res, err := suite.queryClient.DenomHash(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expHash, res.Hash)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestEscrowAddress() {
	var req *types.QueryEscrowAddressRequest

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {
				req = &types.QueryEscrowAddressRequest{
					PortId:    ibctesting.TransferPort,
					ChannelId: ibctesting.FirstChannelID,
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.chainA.GetContext())

			res, err := suite.queryClient.EscrowAddress(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				expected := types.GetEscrowAddress(ibctesting.TransferPort, ibctesting.FirstChannelID).String()
				suite.Require().Equal(expected, res.EscrowAddress)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
