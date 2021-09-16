package keeper_test

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v2/modules/core/05-port/types"
	"github.com/cosmos/ibc-go/v2/testing/mock"
)

func (suite *KeeperTestSuite) TestAppVersion() {
	var (
		req        *types.QueryAppVersionRequest
		expVersion string
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"empty request",
			func() {
				req = nil
			},
			false,
		},
		{
			"invalid port ID",
			func() {
				req = &types.QueryAppVersionRequest{
					PortId: "",
				}
			},
			false,
		},
		{
			"module not found",
			func() {
				req = &types.QueryAppVersionRequest{
					PortId: "mock-port-id",
				}
			},
			false,
		},
		{
			"version negotiation failure",
			func() {

				expVersion = mock.Version

				req = &types.QueryAppVersionRequest{
					PortId: "mock", // retrieves the mock testing module
					Counterparty: &channeltypes.Counterparty{
						PortId:    "mock-port-id",
						ChannelId: "mock-channel-id",
					},
					ProposedVersion: "invalid-proposed-version",
				}
			},
			false,
		},
		{
			"success",
			func() {

				expVersion = mock.Version

				req = &types.QueryAppVersionRequest{
					PortId: "mock", // retrieves the mock testing module
					Counterparty: &channeltypes.Counterparty{
						PortId:    "mock-port-id",
						ChannelId: "mock-channel-id",
					},
					ProposedVersion: mock.Version,
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()

			ctx := sdk.WrapSDKContext(suite.ctx)
			res, err := suite.keeper.AppVersion(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expVersion, res.Version)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
