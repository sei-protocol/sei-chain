package keeper_test

import (
	"time"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

// TestConnOpenInit - chainA initializes (INIT state) a connection with
// chainB which is yet UNINITIALIZED
func (suite *KeeperTestSuite) TestConnOpenInit() {
	var (
		path         *ibctesting.Path
		version      *types.Version
		delayPeriod  uint64
		emptyConnBID bool
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"success", func() {
		}, true},
		{"success with empty counterparty identifier", func() {
			emptyConnBID = true
		}, true},
		{"success with non empty version", func() {
			version = types.ExportedVersionsToProto(types.GetCompatibleVersions())[0]
		}, true},
		{"success with non zero delayPeriod", func() {
			delayPeriod = uint64(time.Hour.Nanoseconds())
		}, true},

		{"invalid version", func() {
			version = &types.Version{}
		}, false},
		{"couldn't add connection to client", func() {
			// set path.EndpointA.ClientID to invalid client identifier
			path.EndpointA.ClientID = "clientidentifier"
		}, false},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.msg, func() {
			suite.SetupTest()    // reset
			emptyConnBID = false // must be explicitly changed
			version = nil        // must be explicitly changed
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)

			tc.malleate()

			if emptyConnBID {
				path.EndpointB.ConnectionID = ""
			}
			counterparty := types.NewCounterparty(path.EndpointB.ClientID, path.EndpointB.ConnectionID, suite.chainB.GetPrefix())

			connectionID, err := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.ConnOpenInit(suite.chainA.GetContext(), path.EndpointA.ClientID, counterparty, version, delayPeriod)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(types.FormatConnectionIdentifier(0), connectionID)
			} else {
				suite.Require().Error(err)
				suite.Require().Equal("", connectionID)
			}
		})
	}
}

// TestConnOpenTry - chainB calls ConnOpenTry to verify the state of
// connection on chainA is INIT
func (suite *KeeperTestSuite) TestConnOpenTry() {
	var (
		path                 *ibctesting.Path
		delayPeriod          uint64
		previousConnectionID string
		versions             []exported.Version
		consensusHeight      exported.Height
		counterpartyClient   exported.ClientState
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"success", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)
		}, true},
		{"success with crossing hellos", func() {
			err := suite.coordinator.ConnOpenInitOnBothChains(path)
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			previousConnectionID = path.EndpointB.ConnectionID
		}, true},
		{"success with delay period", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			delayPeriod = uint64(time.Hour.Nanoseconds())

			// set delay period on counterparty to non-zero value
			conn := path.EndpointA.GetConnection()
			conn.DelayPeriod = delayPeriod
			suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainA.GetContext(), path.EndpointA.ConnectionID, conn)

			// commit in order for proof to return correct value
			suite.coordinator.CommitBlock(suite.chainA)
			path.EndpointB.UpdateClient()

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)
		}, true},
		{"invalid counterparty client", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			// Set an invalid client of chainA on chainB
			tmClient, ok := counterpartyClient.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClient.ChainId = "wrongchainid"

			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID, tmClient)
		}, false},
		{"consensus height >= latest height", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			consensusHeight = clienttypes.GetSelfHeight(suite.chainB.GetContext())
		}, false},
		{"self consensus state not found", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			consensusHeight = clienttypes.NewHeight(0, 1)
		}, false},
		{"counterparty versions is empty", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			versions = nil
		}, false},
		{"counterparty versions don't have a match", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			version := types.NewVersion("0.0", nil)
			versions = []exported.Version{version}
		}, false},
		{"connection state verification failed", func() {
			// chainA connection not created

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)
		}, false},
		{"client state verification failed", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			// modify counterparty client without setting in store so it still passes validate but fails proof verification
			tmClient, ok := counterpartyClient.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClient.LatestHeight = tmClient.LatestHeight.Increment().(clienttypes.Height)
		}, false},
		{"consensus state verification failed", func() {
			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			// give chainA wrong consensus state for chainB
			consState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetLatestClientConsensusState(suite.chainA.GetContext(), path.EndpointA.ClientID)
			suite.Require().True(found)

			tmConsState, ok := consState.(*ibctmtypes.ConsensusState)
			suite.Require().True(ok)

			tmConsState.Timestamp = time.Now()
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), path.EndpointA.ClientID, counterpartyClient.GetLatestHeight(), tmConsState)

			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)
		}, false},
		{"invalid previous connection is in TRYOPEN", func() {
			// open init chainA
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// open try chainB
			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			err = path.EndpointB.UpdateClient()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			previousConnectionID = path.EndpointB.ConnectionID
		}, false},
		{"invalid previous connection has invalid versions", func() {
			// open init chainA
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// open try chainB
			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			// modify connB to be in INIT with incorrect versions
			connection, found := suite.chainB.App.GetIBCKeeper().ConnectionKeeper.GetConnection(suite.chainB.GetContext(), path.EndpointB.ConnectionID)
			suite.Require().True(found)

			connection.State = types.INIT
			connection.Versions = []*types.Version{{}}

			suite.chainB.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainB.GetContext(), path.EndpointB.ConnectionID, connection)

			err = path.EndpointB.UpdateClient()
			suite.Require().NoError(err)

			// retrieve client state of chainA to pass as counterpartyClient
			counterpartyClient = suite.chainA.GetClientState(path.EndpointA.ClientID)

			previousConnectionID = path.EndpointB.ConnectionID
		}, false},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest()                          // reset
			consensusHeight = clienttypes.ZeroHeight() // must be explicitly changed in malleate
			versions = types.GetCompatibleVersions()   // must be explicitly changed in malleate
			previousConnectionID = ""
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)

			tc.malleate()

			counterparty := types.NewCounterparty(path.EndpointA.ClientID, path.EndpointA.ConnectionID, suite.chainA.GetPrefix())

			// ensure client is up to date to receive proof
			err := path.EndpointB.UpdateClient()
			suite.Require().NoError(err)

			connectionKey := host.ConnectionKey(path.EndpointA.ConnectionID)
			proofInit, proofHeight := suite.chainA.QueryProof(connectionKey)

			if consensusHeight.IsZero() {
				// retrieve consensus state height to provide proof for
				consensusHeight = counterpartyClient.GetLatestHeight()
			}
			consensusKey := host.FullConsensusStateKey(path.EndpointA.ClientID, consensusHeight)
			proofConsensus, _ := suite.chainA.QueryProof(consensusKey)

			// retrieve proof of counterparty clientstate on chainA
			clientKey := host.FullClientStateKey(path.EndpointA.ClientID)
			proofClient, _ := suite.chainA.QueryProof(clientKey)

			connectionID, err := suite.chainB.App.GetIBCKeeper().ConnectionKeeper.ConnOpenTry(
				suite.chainB.GetContext(), previousConnectionID, counterparty, delayPeriod, path.EndpointB.ClientID, counterpartyClient,
				versions, proofInit, proofClient, proofConsensus,
				proofHeight, consensusHeight,
			)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(types.FormatConnectionIdentifier(0), connectionID)
			} else {
				suite.Require().Error(err)
				suite.Require().Equal("", connectionID)
			}
		})
	}
}

// TestConnOpenAck - Chain A (ID #1) calls TestConnOpenAck to acknowledge (ACK state)
// the initialization (TRYINIT) of the connection on  Chain B (ID #2).
func (suite *KeeperTestSuite) TestConnOpenAck() {
	var (
		path               *ibctesting.Path
		consensusHeight    exported.Height
		version            *types.Version
		counterpartyClient exported.ClientState
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"success", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)
		}, true},
		{"success from tryopen", func() {
			// chainA is in TRYOPEN, chainB is in TRYOPEN
			err := path.EndpointB.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointA.ConnOpenTry()
			suite.Require().NoError(err)

			// set chainB to TRYOPEN
			connection := path.EndpointB.GetConnection()
			connection.State = types.TRYOPEN
			connection.Counterparty.ConnectionId = path.EndpointA.ConnectionID
			suite.chainB.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainB.GetContext(), path.EndpointB.ConnectionID, connection)
			// update path.EndpointB.ClientID so state change is committed
			path.EndpointB.UpdateClient()

			path.EndpointA.UpdateClient()

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)
		}, true},
		{"invalid counterparty client", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			// Set an invalid client of chainA on chainB
			tmClient, ok := counterpartyClient.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClient.ChainId = "wrongchainid"

			suite.chainB.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainB.GetContext(), path.EndpointB.ClientID, tmClient)

		}, false},
		{"consensus height >= latest height", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			consensusHeight = clienttypes.GetSelfHeight(suite.chainA.GetContext())
		}, false},
		{"connection not found", func() {
			// connections are never created

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)
		}, false},
		{"invalid counterparty connection ID", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			// modify connB to set counterparty connection identifier to wrong identifier
			connection, found := suite.chainA.App.GetIBCKeeper().ConnectionKeeper.GetConnection(suite.chainA.GetContext(), path.EndpointA.ConnectionID)
			suite.Require().True(found)

			connection.Counterparty.ConnectionId = "badconnectionid"

			suite.chainA.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainA.GetContext(), path.EndpointA.ConnectionID, connection)

			err = path.EndpointA.UpdateClient()
			suite.Require().NoError(err)

			err = path.EndpointB.UpdateClient()
			suite.Require().NoError(err)
		}, false},
		{"connection state is not INIT", func() {
			// connection state is already OPEN on chainA
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			err = path.EndpointA.ConnOpenAck()
			suite.Require().NoError(err)
		}, false},
		{"connection is in INIT but the proposed version is invalid", func() {
			// chainA is in INIT, chainB is in TRYOPEN
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			version = types.NewVersion("2.0", nil)
		}, false},
		{"connection is in TRYOPEN but the set version in the connection is invalid", func() {
			// chainA is in TRYOPEN, chainB is in TRYOPEN
			err := path.EndpointB.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointA.ConnOpenTry()
			suite.Require().NoError(err)

			// set chainB to TRYOPEN
			connection := path.EndpointB.GetConnection()
			connection.State = types.TRYOPEN
			suite.chainB.App.GetIBCKeeper().ConnectionKeeper.SetConnection(suite.chainB.GetContext(), path.EndpointB.ConnectionID, connection)

			// update path.EndpointB.ClientID so state change is committed
			path.EndpointB.UpdateClient()
			path.EndpointA.UpdateClient()

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			version = types.NewVersion("2.0", nil)
		}, false},
		{"incompatible IBC versions", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			// set version to a non-compatible version
			version = types.NewVersion("2.0", nil)
		}, false},
		{"empty version", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			version = &types.Version{}
		}, false},
		{"feature set verification failed - unsupported feature", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			version = types.NewVersion(types.DefaultIBCVersionIdentifier, []string{"ORDER_ORDERED", "ORDER_UNORDERED", "ORDER_DAG"})
		}, false},
		{"self consensus state not found", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			consensusHeight = clienttypes.NewHeight(0, 1)
		}, false},
		{"connection state verification failed", func() {
			// chainB connection is not in INIT
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)
		}, false},
		{"client state verification failed", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			// modify counterparty client without setting in store so it still passes validate but fails proof verification
			tmClient, ok := counterpartyClient.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClient.LatestHeight = tmClient.LatestHeight.Increment().(clienttypes.Height)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)
		}, false},
		{"consensus state verification failed", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			// retrieve client state of chainB to pass as counterpartyClient
			counterpartyClient = suite.chainB.GetClientState(path.EndpointB.ClientID)

			// give chainB wrong consensus state for chainA
			consState, found := suite.chainB.App.GetIBCKeeper().ClientKeeper.GetLatestClientConsensusState(suite.chainB.GetContext(), path.EndpointB.ClientID)
			suite.Require().True(found)

			tmConsState, ok := consState.(*ibctmtypes.ConsensusState)
			suite.Require().True(ok)

			tmConsState.Timestamp = tmConsState.Timestamp.Add(time.Second)
			suite.chainB.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainB.GetContext(), path.EndpointB.ClientID, counterpartyClient.GetLatestHeight(), tmConsState)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

		}, false},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.msg, func() {
			suite.SetupTest()                                                         // reset
			version = types.ExportedVersionsToProto(types.GetCompatibleVersions())[0] // must be explicitly changed in malleate
			consensusHeight = clienttypes.ZeroHeight()                                // must be explicitly changed in malleate
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)

			tc.malleate()

			// ensure client is up to date to receive proof
			err := path.EndpointA.UpdateClient()
			suite.Require().NoError(err)

			connectionKey := host.ConnectionKey(path.EndpointB.ConnectionID)
			proofTry, proofHeight := suite.chainB.QueryProof(connectionKey)

			if consensusHeight.IsZero() {
				// retrieve consensus state height to provide proof for
				clientState := suite.chainB.GetClientState(path.EndpointB.ClientID)
				consensusHeight = clientState.GetLatestHeight()
			}
			consensusKey := host.FullConsensusStateKey(path.EndpointB.ClientID, consensusHeight)
			proofConsensus, _ := suite.chainB.QueryProof(consensusKey)

			// retrieve proof of counterparty clientstate on chainA
			clientKey := host.FullClientStateKey(path.EndpointB.ClientID)
			proofClient, _ := suite.chainB.QueryProof(clientKey)

			err = suite.chainA.App.GetIBCKeeper().ConnectionKeeper.ConnOpenAck(
				suite.chainA.GetContext(), path.EndpointA.ConnectionID, counterpartyClient, version, path.EndpointB.ConnectionID,
				proofTry, proofClient, proofConsensus, proofHeight, consensusHeight,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestConnOpenConfirm - chainB calls ConnOpenConfirm to confirm that
// chainA state is now OPEN.
func (suite *KeeperTestSuite) TestConnOpenConfirm() {
	var path *ibctesting.Path
	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"success", func() {
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)

			err = path.EndpointA.ConnOpenAck()
			suite.Require().NoError(err)
		}, true},
		{"connection not found", func() {
			// connections are never created
		}, false},
		{"chain B's connection state is not TRYOPEN", func() {
			// connections are OPEN
			suite.coordinator.CreateConnections(path)
		}, false},
		{"connection state verification failed", func() {
			// chainA is in INIT
			err := path.EndpointA.ConnOpenInit()
			suite.Require().NoError(err)

			err = path.EndpointB.ConnOpenTry()
			suite.Require().NoError(err)
		}, false},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.msg, func() {
			suite.SetupTest() // reset
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)

			tc.malleate()

			// ensure client is up to date to receive proof
			err := path.EndpointB.UpdateClient()
			suite.Require().NoError(err)

			connectionKey := host.ConnectionKey(path.EndpointA.ConnectionID)
			proofAck, proofHeight := suite.chainA.QueryProof(connectionKey)

			err = suite.chainB.App.GetIBCKeeper().ConnectionKeeper.ConnOpenConfirm(
				suite.chainB.GetContext(), path.EndpointB.ConnectionID, proofAck, proofHeight,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
