package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

type KeeperTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) TestSetAndGetConnection() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path)
	firstConnection := "connection-0"

	// check first connection does not exist
	_, existed := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetConnection(suite.chainA.GetContext(), firstConnection)
	suite.Require().False(existed)

	suite.coordinator.CreateConnections(path)
	_, existed = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetConnection(suite.chainA.GetContext(), firstConnection)
	suite.Require().True(existed)
}

func (suite *KeeperTestSuite) TestSetAndGetClientConnectionPaths() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path)

	_, existed := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetClientConnectionPaths(suite.chainA.GetContext(), path.EndpointA.ClientID)
	suite.False(existed)

	connections := []string{"connectionA", "connectionB"}
	suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetClientConnectionPaths(suite.chainA.GetContext(), path.EndpointA.ClientID, connections)
	paths, existed := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetClientConnectionPaths(suite.chainA.GetContext(), path.EndpointA.ClientID)
	suite.True(existed)
	suite.EqualValues(connections, paths)
}

// create 2 connections: A0 - B0, A1 - B1
func (suite KeeperTestSuite) TestGetAllConnections() {
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path1)

	path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path2.EndpointA.ClientID = path1.EndpointA.ClientID
	path2.EndpointB.ClientID = path1.EndpointB.ClientID

	suite.coordinator.CreateConnections(path2)

	counterpartyB0 := types.NewCounterparty(path1.EndpointB.ClientID, path1.EndpointB.ConnectionID, suite.chainB.GetPrefix()) // connection B0
	counterpartyB1 := types.NewCounterparty(path2.EndpointB.ClientID, path2.EndpointB.ConnectionID, suite.chainB.GetPrefix()) // connection B1

	conn1 := types.NewConnectionEnd(types.OPEN, path1.EndpointA.ClientID, counterpartyB0, types.ExportedVersionsToProto(types.GetCompatibleVersions()), 0) // A0 - B0
	conn2 := types.NewConnectionEnd(types.OPEN, path2.EndpointA.ClientID, counterpartyB1, types.ExportedVersionsToProto(types.GetCompatibleVersions()), 0) // A1 - B1

	iconn1 := types.NewIdentifiedConnection(path1.EndpointA.ConnectionID, conn1)
	iconn2 := types.NewIdentifiedConnection(path2.EndpointA.ConnectionID, conn2)

	expConnections := []types.IdentifiedConnection{iconn1, iconn2}

	connections := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetAllConnections(suite.chainA.GetContext())
	suite.Require().Len(connections, len(expConnections))
	suite.Require().Equal(expConnections, connections)
}

// the test creates 2 clients path.EndpointA.ClientID0 and path.EndpointA.ClientID1. path.EndpointA.ClientID0 has a single
// connection and path.EndpointA.ClientID1 has 2 connections.
func (suite KeeperTestSuite) TestGetAllClientConnectionPaths() {
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path1)
	suite.coordinator.SetupConnections(path2)

	path3 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path3.EndpointA.ClientID = path2.EndpointA.ClientID
	path3.EndpointB.ClientID = path2.EndpointB.ClientID
	suite.coordinator.CreateConnections(path3)

	expPaths := []types.ConnectionPaths{
		types.NewConnectionPaths(path1.EndpointA.ClientID, []string{path1.EndpointA.ConnectionID}),
		types.NewConnectionPaths(path2.EndpointA.ClientID, []string{path2.EndpointA.ConnectionID, path3.EndpointA.ConnectionID}),
	}

	connPaths := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetAllClientConnectionPaths(suite.chainA.GetContext())
	suite.Require().Len(connPaths, 2)
	suite.Require().Equal(expPaths, connPaths)
}

// TestGetTimestampAtHeight verifies if the clients on each chain return the
// correct timestamp for the other chain.
func (suite *KeeperTestSuite) TestGetTimestampAtHeight() {
	var connection types.ConnectionEnd

	cases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"verification success", func() {
			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)
			connection = path.EndpointA.GetConnection()
		}, true},
		{"consensus state not found", func() {
			// any non-nil value of connection is valid
			suite.Require().NotNil(connection)
		}, false},
	}

	for _, tc := range cases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()

			actualTimestamp, err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetTimestampAtHeight(
				suite.chainA.GetContext(), connection, suite.chainB.LastHeader.GetHeight(),
			)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().EqualValues(uint64(suite.chainB.LastHeader.GetTime().UnixNano()), actualTimestamp)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
