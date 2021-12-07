package types_test

import (
	"fmt"

	"github.com/cosmos/ibc-go/v2/modules/apps/27-interchain-accounts/types"
	ibctesting "github.com/cosmos/ibc-go/v2/testing"
)

func (suite *TypesTestSuite) TestGeneratePortID() {
	var (
		path  *ibctesting.Path
		owner = TestOwnerAddress
	)

	testCases := []struct {
		name     string
		malleate func()
		expValue string
		expPass  bool
	}{
		{
			"success",
			func() {},
			fmt.Sprint(types.VersionPrefix, types.Delimiter, "0", types.Delimiter, "0", types.Delimiter, TestOwnerAddress),
			true,
		},
		{
			"success with non matching connection sequences",
			func() {
				path.EndpointA.ConnectionID = "connection-1"
			},
			fmt.Sprint(types.VersionPrefix, types.Delimiter, "1", types.Delimiter, "0", types.Delimiter, TestOwnerAddress),
			true,
		},
		{
			"invalid connectionID",
			func() {
				path.EndpointA.ConnectionID = "connection"
			},
			"",
			false,
		},
		{
			"invalid counterparty connectionID",
			func() {
				path.EndpointB.ConnectionID = "connection"
			},
			"",
			false,
		},
		{
			"invalid owner address",
			func() {
				owner = "    "
			},
			"",
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.Setup(path)

			tc.malleate() // malleate mutates test data

			portID, err := types.GeneratePortID(owner, path.EndpointA.ConnectionID, path.EndpointB.ConnectionID)

			if tc.expPass {
				suite.Require().NoError(err, tc.name)
				suite.Require().Equal(tc.expValue, portID)
			} else {
				suite.Require().Error(err, tc.name)
				suite.Require().Empty(portID)
			}
		})
	}
}

func (suite *TypesTestSuite) TestParseControllerConnSequence() {

	testCases := []struct {
		name     string
		portID   string
		expValue uint64
		expPass  bool
	}{
		{
			"success",
			TestPortID,
			0,
			true,
		},
		{
			"failed to parse port identifier",
			"invalid-port-id",
			0,
			false,
		},
		{
			"failed to parse connection sequence",
			"ics27-1.x.y.cosmos1",
			0,
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			connSeq, err := types.ParseControllerConnSequence(tc.portID)

			if tc.expPass {
				suite.Require().Equal(tc.expValue, connSeq)
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Zero(connSeq)
				suite.Require().Error(err, tc.name)
			}
		})
	}
}

func (suite *TypesTestSuite) TestParseHostConnSequence() {

	testCases := []struct {
		name     string
		portID   string
		expValue uint64
		expPass  bool
	}{
		{
			"success",
			TestPortID,
			0,
			true,
		},
		{
			"failed to parse port identifier",
			"invalid-port-id",
			0,
			false,
		},
		{
			"failed to parse connection sequence",
			"ics27-1.x.y.cosmos1",
			0,
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			connSeq, err := types.ParseHostConnSequence(tc.portID)

			if tc.expPass {
				suite.Require().Equal(tc.expValue, connSeq)
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Zero(connSeq)
				suite.Require().Error(err, tc.name)
			}
		})
	}
}
