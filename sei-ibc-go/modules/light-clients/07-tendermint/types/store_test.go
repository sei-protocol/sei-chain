package types_test

import (
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
	solomachinetypes "github.com/cosmos/ibc-go/modules/light-clients/06-solomachine/types"
	"github.com/cosmos/ibc-go/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/testing"
)

func (suite *TendermintTestSuite) TestGetConsensusState() {
	var (
		height exported.Height
		path   *ibctesting.Path
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success", func() {}, true,
		},
		{
			"consensus state not found", func() {
				// use height with no consensus state set
				height = height.(clienttypes.Height).Increment()
			}, false,
		},
		{
			"not a consensus state interface", func() {
				// marshal an empty client state and set as consensus state
				store := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
				clientStateBz := suite.chainA.App.GetIBCKeeper().ClientKeeper.MustMarshalClientState(&types.ClientState{})
				store.Set(host.ConsensusStateKey(height), clientStateBz)
			}, false,
		},
		{
			"invalid consensus state (solomachine)", func() {
				// marshal and set solomachine consensus state
				store := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
				consensusStateBz := suite.chainA.App.GetIBCKeeper().ClientKeeper.MustMarshalConsensusState(&solomachinetypes.ConsensusState{})
				store.Set(host.ConsensusStateKey(height), consensusStateBz)
			}, false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest()
			path = ibctesting.NewPath(suite.chainA, suite.chainB)

			suite.coordinator.Setup(path)
			clientState := suite.chainA.GetClientState(path.EndpointA.ClientID)
			height = clientState.GetLatestHeight()

			tc.malleate() // change vars as necessary

			store := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
			consensusState, err := types.GetConsensusState(store, suite.chainA.Codec, height)

			if tc.expPass {
				suite.Require().NoError(err)
				expConsensusState, found := suite.chainA.GetConsensusState(path.EndpointA.ClientID, height)
				suite.Require().True(found)
				suite.Require().Equal(expConsensusState, consensusState)
			} else {
				suite.Require().Error(err)
				suite.Require().Nil(consensusState)
			}
		})
	}
}

func (suite *TendermintTestSuite) TestGetProcessedTime() {
	// setup
	path := ibctesting.NewPath(suite.chainA, suite.chainB)

	suite.coordinator.UpdateTime()
	// coordinator increments time before creating client
	expectedTime := suite.chainA.CurrentHeader.Time.Add(ibctesting.TimeIncrement)

	// Verify ProcessedTime on CreateClient
	err := path.EndpointA.CreateClient()
	suite.Require().NoError(err)

	clientState := suite.chainA.GetClientState(path.EndpointA.ClientID)
	height := clientState.GetLatestHeight()

	store := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
	actualTime, ok := types.GetProcessedTime(store, height)
	suite.Require().True(ok, "could not retrieve processed time for stored consensus state")
	suite.Require().Equal(uint64(expectedTime.UnixNano()), actualTime, "retrieved processed time is not expected value")

	suite.coordinator.UpdateTime()
	// coordinator increments time before updating client
	expectedTime = suite.chainA.CurrentHeader.Time.Add(ibctesting.TimeIncrement)

	// Verify ProcessedTime on UpdateClient
	err = path.EndpointA.UpdateClient()
	suite.Require().NoError(err)

	clientState = suite.chainA.GetClientState(path.EndpointA.ClientID)
	height = clientState.GetLatestHeight()

	store = suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
	actualTime, ok = types.GetProcessedTime(store, height)
	suite.Require().True(ok, "could not retrieve processed time for stored consensus state")
	suite.Require().Equal(uint64(expectedTime.UnixNano()), actualTime, "retrieved processed time is not expected value")

	// try to get processed time for height that doesn't exist in store
	_, ok = types.GetProcessedTime(store, clienttypes.NewHeight(1, 1))
	suite.Require().False(ok, "retrieved processed time for a non-existent consensus state")
}
