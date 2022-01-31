package keeper_test

import (
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *KeeperTestSuite) TestClientUpdateProposal() {
	var (
		subject, substitute                       string
		subjectClientState, substituteClientState exported.ClientState
		content                                   govtypes.Content
		err                                       error
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"valid update client proposal", func() {
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, true,
		},
		{
			"subject and substitute use different revision numbers", func() {
				tmClientState, ok := substituteClientState.(*ibctmtypes.ClientState)
				suite.Require().True(ok)
				consState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientConsensusState(suite.chainA.GetContext(), substitute, tmClientState.LatestHeight)
				suite.Require().True(found)
				newRevisionNumber := tmClientState.GetLatestHeight().GetRevisionNumber() + 1

				tmClientState.LatestHeight = types.NewHeight(newRevisionNumber, tmClientState.GetLatestHeight().GetRevisionHeight())

				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), substitute, tmClientState.LatestHeight, consState)
				clientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), substitute)
				ibctmtypes.SetProcessedTime(clientStore, tmClientState.LatestHeight, 100)
				ibctmtypes.SetProcessedHeight(clientStore, tmClientState.LatestHeight, types.NewHeight(0, 1))
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), substitute, tmClientState)

				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, true,
		},
		{
			"cannot use localhost as subject", func() {
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, exported.Localhost, substitute)
			}, false,
		},
		{
			"cannot use localhost as substitute", func() {
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, exported.Localhost)
			}, false,
		},
		{
			"cannot use solomachine as substitute for tendermint client", func() {
				solomachine := ibctesting.NewSolomachine(suite.T(), suite.cdc, "solo machine", "", 1)
				solomachine.Sequence = subjectClientState.GetLatestHeight().GetRevisionHeight() + 1
				substituteClientState = solomachine.ClientState()
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), substitute, substituteClientState)
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, false,
		},
		{
			"subject client does not exist", func() {
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, ibctesting.InvalidID, substitute)
			}, false,
		},
		{
			"substitute client does not exist", func() {
				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, ibctesting.InvalidID)
			}, false,
		},
		{
			"subject and substitute have equal latest height", func() {
				tmClientState, ok := subjectClientState.(*ibctmtypes.ClientState)
				suite.Require().True(ok)
				tmClientState.LatestHeight = substituteClientState.GetLatestHeight().(types.Height)
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), subject, tmClientState)

				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, false,
		},
		{
			"update fails, client is not frozen or expired", func() {
				tmClientState, ok := subjectClientState.(*ibctmtypes.ClientState)
				suite.Require().True(ok)
				tmClientState.FrozenHeight = types.ZeroHeight()
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), subject, tmClientState)

				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, false,
		},
		{
			"substitute is frozen", func() {
				tmClientState, ok := substituteClientState.(*ibctmtypes.ClientState)
				suite.Require().True(ok)
				tmClientState.FrozenHeight = types.NewHeight(0, 1)
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), substitute, tmClientState)

				content = types.NewClientUpdateProposal(ibctesting.Title, ibctesting.Description, subject, substitute)
			}, false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			subjectPath := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(subjectPath)
			subject = subjectPath.EndpointA.ClientID
			subjectClientState = suite.chainA.GetClientState(subject)

			substitutePath := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(substitutePath)
			substitute = substitutePath.EndpointA.ClientID

			// update substitute twice
			substitutePath.EndpointA.UpdateClient()
			substitutePath.EndpointA.UpdateClient()
			substituteClientState = suite.chainA.GetClientState(substitute)

			tmClientState, ok := subjectClientState.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClientState.AllowUpdateAfterMisbehaviour = true
			tmClientState.AllowUpdateAfterExpiry = true
			tmClientState.FrozenHeight = tmClientState.LatestHeight
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), subject, tmClientState)

			tmClientState, ok = substituteClientState.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClientState.AllowUpdateAfterMisbehaviour = true
			tmClientState.AllowUpdateAfterExpiry = true
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), substitute, tmClientState)

			tc.malleate()

			updateProp, ok := content.(*types.ClientUpdateProposal)
			suite.Require().True(ok)
			err = suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientUpdateProposal(suite.chainA.GetContext(), updateProp)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}

}

func (suite *KeeperTestSuite) TestHandleUpgradeProposal() {
	var (
		upgradedClientState *ibctmtypes.ClientState
		oldPlan, plan       upgradetypes.Plan
		content             govtypes.Content
		err                 error
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"valid upgrade proposal", func() {
				content, err = types.NewUpgradeProposal(ibctesting.Title, ibctesting.Description, plan, upgradedClientState)
				suite.Require().NoError(err)
			}, true,
		},
		{
			"valid upgrade proposal with previous IBC state", func() {
				oldPlan = upgradetypes.Plan{
					Name:   "upgrade IBC clients",
					Height: 100,
				}

				content, err = types.NewUpgradeProposal(ibctesting.Title, ibctesting.Description, plan, upgradedClientState)
				suite.Require().NoError(err)
			}, true,
		},
		{
			"cannot unpack client state", func() {
				any, err := types.PackConsensusState(&ibctmtypes.ConsensusState{})
				suite.Require().NoError(err)
				content = &types.UpgradeProposal{
					Title:               ibctesting.Title,
					Description:         ibctesting.Description,
					Plan:                plan,
					UpgradedClientState: any,
				}
			}, false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest()  // reset
			oldPlan.Height = 0 //reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)
			upgradedClientState = suite.chainA.GetClientState(path.EndpointA.ClientID).ZeroCustomFields().(*ibctmtypes.ClientState)

			// use height 1000 to distinguish from old plan
			plan = upgradetypes.Plan{
				Name:   "upgrade IBC clients",
				Height: 1000,
			}

			tc.malleate()

			// set the old plan if it is not empty
			if oldPlan.Height != 0 {
				// set upgrade plan in the upgrade store
				store := suite.chainA.GetContext().KVStore(suite.chainA.GetSimApp().GetKey(upgradetypes.StoreKey))
				bz := suite.chainA.App.AppCodec().MustMarshal(&oldPlan)
				store.Set(upgradetypes.PlanKey(), bz)

				bz, err := types.MarshalClientState(suite.chainA.App.AppCodec(), upgradedClientState)
				suite.Require().NoError(err)
				suite.chainA.GetSimApp().UpgradeKeeper.SetUpgradedClient(suite.chainA.GetContext(), oldPlan.Height, bz)
			}

			upgradeProp, ok := content.(*types.UpgradeProposal)
			suite.Require().True(ok)
			err = suite.chainA.App.GetIBCKeeper().ClientKeeper.HandleUpgradeProposal(suite.chainA.GetContext(), upgradeProp)

			if tc.expPass {
				suite.Require().NoError(err)

				// check that the correct plan is returned
				storedPlan, found := suite.chainA.GetSimApp().UpgradeKeeper.GetUpgradePlan(suite.chainA.GetContext())
				suite.Require().True(found)
				suite.Require().Equal(plan, storedPlan)

				// check that old upgraded client state is cleared
				_, found = suite.chainA.GetSimApp().UpgradeKeeper.GetUpgradedClient(suite.chainA.GetContext(), oldPlan.Height)
				suite.Require().False(found)

				// check that client state was set
				storedClientState, found := suite.chainA.GetSimApp().UpgradeKeeper.GetUpgradedClient(suite.chainA.GetContext(), plan.Height)
				suite.Require().True(found)
				clientState, err := types.UnmarshalClientState(suite.chainA.App.AppCodec(), storedClientState)
				suite.Require().NoError(err)
				suite.Require().Equal(upgradedClientState, clientState)
			} else {
				suite.Require().Error(err)

				// check that the new plan wasn't stored
				storedPlan, found := suite.chainA.GetSimApp().UpgradeKeeper.GetUpgradePlan(suite.chainA.GetContext())
				if oldPlan.Height != 0 {
					// NOTE: this is only true if the ScheduleUpgrade function
					// returns an error before clearing the old plan
					suite.Require().True(found)
					suite.Require().Equal(oldPlan, storedPlan)
				} else {
					suite.Require().False(found)
					suite.Require().Empty(storedPlan)
				}

				// check that client state was not set
				_, found = suite.chainA.GetSimApp().UpgradeKeeper.GetUpgradedClient(suite.chainA.GetContext(), plan.Height)
				suite.Require().False(found)

			}
		})
	}

}
