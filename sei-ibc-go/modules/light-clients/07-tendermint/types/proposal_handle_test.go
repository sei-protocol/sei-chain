package types_test

import (
	"time"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	"github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

var (
	frozenHeight = clienttypes.NewHeight(0, 1)
)

func (suite *TendermintTestSuite) TestCheckSubstituteUpdateStateBasic() {
	var (
		substituteClientState exported.ClientState
		substitutePath        *ibctesting.Path
	)
	testCases := []struct {
		name     string
		malleate func()
	}{
		{
			"solo machine used for substitute", func() {
				substituteClientState = ibctesting.NewSolomachine(suite.T(), suite.cdc, "solo machine", "", 1).ClientState()
			},
		},
		{
			"non-matching substitute", func() {
				suite.coordinator.SetupClients(substitutePath)
				substituteClientState = suite.chainA.GetClientState(substitutePath.EndpointA.ClientID).(*types.ClientState)
				tmClientState, ok := substituteClientState.(*types.ClientState)
				suite.Require().True(ok)

				tmClientState.ChainId = tmClientState.ChainId + "different chain"
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {

			suite.SetupTest() // reset
			subjectPath := ibctesting.NewPath(suite.chainA, suite.chainB)
			substitutePath = ibctesting.NewPath(suite.chainA, suite.chainB)

			suite.coordinator.SetupClients(subjectPath)
			subjectClientState := suite.chainA.GetClientState(subjectPath.EndpointA.ClientID).(*types.ClientState)
			subjectClientState.AllowUpdateAfterMisbehaviour = true
			subjectClientState.AllowUpdateAfterExpiry = true

			// expire subject client
			suite.coordinator.IncrementTimeBy(subjectClientState.TrustingPeriod)
			suite.coordinator.CommitBlock(suite.chainA, suite.chainB)

			tc.malleate()

			subjectClientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), subjectPath.EndpointA.ClientID)
			substituteClientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), substitutePath.EndpointA.ClientID)

			updatedClient, err := subjectClientState.CheckSubstituteAndUpdateState(suite.chainA.GetContext(), suite.chainA.App.AppCodec(), subjectClientStore, substituteClientStore, substituteClientState)
			suite.Require().Error(err)
			suite.Require().Nil(updatedClient)
		})
	}
}

// to expire clients, time needs to be fast forwarded on both chainA and chainB.
// this is to prevent headers from failing when attempting to update later.
func (suite *TendermintTestSuite) TestCheckSubstituteAndUpdateState() {
	testCases := []struct {
		name                         string
		AllowUpdateAfterExpiry       bool
		AllowUpdateAfterMisbehaviour bool
		FreezeClient                 bool
		ExpireClient                 bool
		expPass                      bool
	}{
		{
			name:                         "not allowed to be updated, not frozen or expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 false,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "not allowed to be updated, client is frozen",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 true,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "not allowed to be updated, client is expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 false,
			ExpireClient:                 true,
			expPass:                      false,
		},
		{
			name:                         "not allowed to be updated, client is frozen and expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 true,
			ExpireClient:                 true,
			expPass:                      false,
		},
		{
			name:                         "allowed to be updated only after misbehaviour, not frozen or expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 false,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "allowed to be updated only after misbehaviour, client is expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 false,
			ExpireClient:                 true,
			expPass:                      false,
		},
		{
			name:                         "allowed to be updated only after expiry, not frozen or expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 false,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "allowed to be updated only after expiry, client is frozen",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 true,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "PASS: allowed to be updated only after misbehaviour, client is frozen",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 true,
			ExpireClient:                 false,
			expPass:                      true,
		},
		{
			name:                         "PASS: allowed to be updated only after misbehaviour, client is frozen and expired",
			AllowUpdateAfterExpiry:       false,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 true,
			ExpireClient:                 true,
			expPass:                      true,
		},
		{
			name:                         "PASS: allowed to be updated only after expiry, client is expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 false,
			ExpireClient:                 true,
			expPass:                      true,
		},
		{
			name:                         "allowed to be updated only after expiry, client is frozen and expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: false,
			FreezeClient:                 true,
			ExpireClient:                 true,
			expPass:                      false,
		},
		{
			name:                         "allowed to be updated after expiry and misbehaviour, not frozen or expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 false,
			ExpireClient:                 false,
			expPass:                      false,
		},
		{
			name:                         "PASS: allowed to be updated after expiry and misbehaviour, client is frozen",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 true,
			ExpireClient:                 false,
			expPass:                      true,
		},
		{
			name:                         "PASS: allowed to be updated after expiry and misbehaviour, client is expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 false,
			ExpireClient:                 true,
			expPass:                      true,
		},
		{
			name:                         "PASS: allowed to be updated after expiry and misbehaviour, client is frozen and expired",
			AllowUpdateAfterExpiry:       true,
			AllowUpdateAfterMisbehaviour: true,
			FreezeClient:                 true,
			ExpireClient:                 true,
			expPass:                      true,
		},
	}

	for _, tc := range testCases {
		tc := tc

		// for each test case a header used for unexpiring clients and unfreezing
		// a client are each tested to ensure that unexpiry headers cannot update
		// a client when a unfreezing header is required.
		suite.Run(tc.name, func() {

			// start by testing unexpiring the client
			suite.SetupTest() // reset

			// construct subject using test case parameters
			subjectPath := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(subjectPath)
			subjectClientState := suite.chainA.GetClientState(subjectPath.EndpointA.ClientID).(*types.ClientState)
			subjectClientState.AllowUpdateAfterExpiry = tc.AllowUpdateAfterExpiry
			subjectClientState.AllowUpdateAfterMisbehaviour = tc.AllowUpdateAfterMisbehaviour

			// apply freezing or expiry as determined by the test case
			if tc.FreezeClient {
				subjectClientState.FrozenHeight = frozenHeight
			}
			if tc.ExpireClient {
				// expire subject client
				suite.coordinator.IncrementTimeBy(subjectClientState.TrustingPeriod)
				suite.coordinator.CommitBlock(suite.chainA, suite.chainB)
			}

			// construct the substitute to match the subject client
			// NOTE: the substitute is explicitly created after the freezing or expiry occurs,
			// primarily to prevent the substitute from becoming frozen. It also should be
			// the natural flow of events in practice. The subject will become frozen/expired
			// and a substitute will be created along with a governance proposal as a response

			substitutePath := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(substitutePath)
			substituteClientState := suite.chainA.GetClientState(substitutePath.EndpointA.ClientID).(*types.ClientState)
			substituteClientState.AllowUpdateAfterExpiry = tc.AllowUpdateAfterExpiry
			substituteClientState.AllowUpdateAfterMisbehaviour = tc.AllowUpdateAfterMisbehaviour
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), substitutePath.EndpointA.ClientID, substituteClientState)

			// update substitute a few times
			for i := 0; i < 3; i++ {
				err := substitutePath.EndpointA.UpdateClient()
				suite.Require().NoError(err)
				// skip a block
				suite.coordinator.CommitBlock(suite.chainA, suite.chainB)
			}

			// get updated substitute
			substituteClientState = suite.chainA.GetClientState(substitutePath.EndpointA.ClientID).(*types.ClientState)

			// test that subject gets updated chain-id
			newChainID := "new-chain-id"
			substituteClientState.ChainId = newChainID

			subjectClientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), subjectPath.EndpointA.ClientID)
			substituteClientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), substitutePath.EndpointA.ClientID)

			expectedConsState := substitutePath.EndpointA.GetConsensusState(substituteClientState.GetLatestHeight())
			expectedProcessedTime, found := types.GetProcessedTime(substituteClientStore, substituteClientState.GetLatestHeight())
			suite.Require().True(found)
			expectedProcessedHeight, found := types.GetProcessedTime(substituteClientStore, substituteClientState.GetLatestHeight())
			suite.Require().True(found)
			expectedIterationKey := types.GetIterationKey(substituteClientStore, substituteClientState.GetLatestHeight())

			updatedClient, err := subjectClientState.CheckSubstituteAndUpdateState(suite.chainA.GetContext(), suite.chainA.App.AppCodec(), subjectClientStore, substituteClientStore, substituteClientState)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(clienttypes.ZeroHeight(), updatedClient.(*types.ClientState).FrozenHeight)

				subjectClientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), subjectPath.EndpointA.ClientID)

				// check that the correct consensus state was copied over
				suite.Require().Equal(substituteClientState.GetLatestHeight(), updatedClient.GetLatestHeight())
				subjectConsState := subjectPath.EndpointA.GetConsensusState(updatedClient.GetLatestHeight())
				subjectProcessedTime, found := types.GetProcessedTime(subjectClientStore, updatedClient.GetLatestHeight())
				suite.Require().True(found)
				subjectProcessedHeight, found := types.GetProcessedTime(substituteClientStore, updatedClient.GetLatestHeight())
				suite.Require().True(found)
				subjectIterationKey := types.GetIterationKey(substituteClientStore, updatedClient.GetLatestHeight())

				suite.Require().Equal(expectedConsState, subjectConsState)
				suite.Require().Equal(expectedProcessedTime, subjectProcessedTime)
				suite.Require().Equal(expectedProcessedHeight, subjectProcessedHeight)
				suite.Require().Equal(expectedIterationKey, subjectIterationKey)

				suite.Require().Equal(newChainID, updatedClient.(*types.ClientState).ChainId)
			} else {
				suite.Require().Error(err)
				suite.Require().Nil(updatedClient)
			}

		})
	}
}

func (suite *TendermintTestSuite) TestIsMatchingClientState() {
	var (
		subjectPath, substitutePath               *ibctesting.Path
		subjectClientState, substituteClientState *types.ClientState
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"matching clients", func() {
				subjectClientState = suite.chainA.GetClientState(subjectPath.EndpointA.ClientID).(*types.ClientState)
				substituteClientState = suite.chainA.GetClientState(substitutePath.EndpointA.ClientID).(*types.ClientState)
			}, true,
		},
		{
			"matching, frozen height is not used in check for equality", func() {
				subjectClientState.FrozenHeight = frozenHeight
				substituteClientState.FrozenHeight = clienttypes.ZeroHeight()
			}, true,
		},
		{
			"matching, latest height is not used in check for equality", func() {
				subjectClientState.LatestHeight = clienttypes.NewHeight(0, 10)
				substituteClientState.FrozenHeight = clienttypes.ZeroHeight()
			}, true,
		},
		{
			"matching, chain id is different", func() {
				subjectClientState.ChainId = "bitcoin"
				substituteClientState.ChainId = "ethereum"
			}, true,
		},
		{
			"not matching, trusting period is different", func() {
				subjectClientState.TrustingPeriod = time.Duration(time.Hour * 10)
				substituteClientState.TrustingPeriod = time.Duration(time.Hour * 1)
			}, false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			subjectPath = ibctesting.NewPath(suite.chainA, suite.chainB)
			substitutePath = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(subjectPath)
			suite.coordinator.SetupClients(substitutePath)

			tc.malleate()

			suite.Require().Equal(tc.expPass, types.IsMatchingClientState(*subjectClientState, *substituteClientState))

		})
	}
}
