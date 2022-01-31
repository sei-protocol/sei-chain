package keeper_test

import (
	"encoding/hex"
	"fmt"
	"time"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v3/modules/core/23-commitment/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	localhosttypes "github.com/cosmos/ibc-go/v3/modules/light-clients/09-localhost/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	ibctestingmock "github.com/cosmos/ibc-go/v3/testing/mock"
)

func (suite *KeeperTestSuite) TestCreateClient() {
	cases := []struct {
		msg         string
		clientState exported.ClientState
		expPass     bool
	}{
		{"success", ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false), true},
		{"client type not supported", localhosttypes.NewClientState(testChainID, clienttypes.NewHeight(0, 1)), false},
	}

	for i, tc := range cases {

		clientID, err := suite.keeper.CreateClient(suite.ctx, tc.clientState, suite.consensusState)
		if tc.expPass {
			suite.Require().NoError(err, "valid test case %d failed: %s", i, tc.msg)
			suite.Require().NotNil(clientID, "valid test case %d failed: %s", i, tc.msg)
		} else {
			suite.Require().Error(err, "invalid test case %d passed: %s", i, tc.msg)
			suite.Require().Equal("", clientID, "invalid test case %d passed: %s", i, tc.msg)
		}
	}
}

func (suite *KeeperTestSuite) TestUpdateClientTendermint() {
	var (
		path         *ibctesting.Path
		updateHeader *ibctmtypes.Header
	)

	// Must create header creation functions since suite.header gets recreated on each test case
	createFutureUpdateFn := func(trustedHeight clienttypes.Height) *ibctmtypes.Header {
		header, err := suite.chainA.ConstructUpdateTMClientHeaderWithTrustedHeight(path.EndpointB.Chain, path.EndpointA.ClientID, trustedHeight)
		suite.Require().NoError(err)
		return header
	}
	createPastUpdateFn := func(fillHeight, trustedHeight clienttypes.Height) *ibctmtypes.Header {
		consState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientConsensusState(suite.chainA.GetContext(), path.EndpointA.ClientID, trustedHeight)
		suite.Require().True(found)

		return suite.chainB.CreateTMClientHeader(suite.chainB.ChainID, int64(fillHeight.RevisionHeight), trustedHeight, consState.(*ibctmtypes.ConsensusState).Timestamp.Add(time.Second*5),
			suite.chainB.Vals, suite.chainB.Vals, suite.chainB.Signers)
	}

	cases := []struct {
		name      string
		malleate  func()
		expPass   bool
		expFreeze bool
	}{
		{"valid update", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			trustHeight := clientState.GetLatestHeight().(types.Height)

			// store intermediate consensus state to check that trustedHeight does not need to be highest consensus state before header height
			path.EndpointA.UpdateClient()

			updateHeader = createFutureUpdateFn(trustHeight)
		}, true, false},
		{"valid past update", func() {
			clientState := path.EndpointA.GetClientState()
			trustedHeight := clientState.GetLatestHeight().(types.Height)

			currHeight := suite.chainB.CurrentHeader.Height
			fillHeight := types.NewHeight(clientState.GetLatestHeight().GetRevisionNumber(), uint64(currHeight))

			// commit a couple blocks to allow client to fill in gaps
			suite.coordinator.CommitBlock(suite.chainB) // this height is not filled in yet
			suite.coordinator.CommitBlock(suite.chainB) // this height is filled in by the update below

			path.EndpointA.UpdateClient()

			// ensure fill height not set
			_, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientConsensusState(suite.chainA.GetContext(), path.EndpointA.ClientID, fillHeight)
			suite.Require().False(found)

			// updateHeader will fill in consensus state between prevConsState and suite.consState
			// clientState should not be updated
			updateHeader = createPastUpdateFn(fillHeight, trustedHeight)
		}, true, false},
		{"valid duplicate update", func() {
			clientID := path.EndpointA.ClientID

			height1 := types.NewHeight(0, 1)

			// store previous consensus state
			prevConsState := &ibctmtypes.ConsensusState{
				Timestamp:          suite.past,
				NextValidatorsHash: suite.chainB.Vals.Hash(),
			}
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, height1, prevConsState)

			height5 := types.NewHeight(0, 5)
			// store next consensus state to check that trustedHeight does not need to be hightest consensus state before header height
			nextConsState := &ibctmtypes.ConsensusState{
				Timestamp:          suite.past.Add(time.Minute),
				NextValidatorsHash: suite.chainB.Vals.Hash(),
			}
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, height5, nextConsState)

			height3 := types.NewHeight(0, 3)
			// updateHeader will fill in consensus state between prevConsState and suite.consState
			// clientState should not be updated
			updateHeader = createPastUpdateFn(height3, height1)
			// set updateHeader's consensus state in store to create duplicate UpdateClient scenario
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, updateHeader.GetHeight(), updateHeader.ConsensusState())
		}, true, false},
		{"misbehaviour detection: conflicting header", func() {
			clientID := path.EndpointA.ClientID

			height1 := types.NewHeight(0, 1)
			// store previous consensus state
			prevConsState := &ibctmtypes.ConsensusState{
				Timestamp:          suite.past,
				NextValidatorsHash: suite.chainB.Vals.Hash(),
			}
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, height1, prevConsState)

			height5 := types.NewHeight(0, 5)
			// store next consensus state to check that trustedHeight does not need to be hightest consensus state before header height
			nextConsState := &ibctmtypes.ConsensusState{
				Timestamp:          suite.past.Add(time.Minute),
				NextValidatorsHash: suite.chainB.Vals.Hash(),
			}
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, height5, nextConsState)

			height3 := types.NewHeight(0, 3)
			// updateHeader will fill in consensus state between prevConsState and suite.consState
			// clientState should not be updated
			updateHeader = createPastUpdateFn(height3, height1)
			// set conflicting consensus state in store to create misbehaviour scenario
			conflictConsState := updateHeader.ConsensusState()
			conflictConsState.Root = commitmenttypes.NewMerkleRoot([]byte("conflicting apphash"))
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, updateHeader.GetHeight(), conflictConsState)
		}, true, true},
		{"misbehaviour detection: monotonic time violation", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientID := path.EndpointA.ClientID
			trustedHeight := clientState.GetLatestHeight().(types.Height)

			// store intermediate consensus state at a time greater than updateHeader time
			// this will break time monotonicity
			incrementedClientHeight := clientState.GetLatestHeight().Increment().(types.Height)
			intermediateConsState := &ibctmtypes.ConsensusState{
				Timestamp:          suite.coordinator.CurrentTime.Add(2 * time.Hour),
				NextValidatorsHash: suite.chainB.Vals.Hash(),
			}
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientConsensusState(suite.chainA.GetContext(), clientID, incrementedClientHeight, intermediateConsState)
			// set iteration key
			clientStore := suite.keeper.ClientStore(suite.ctx, clientID)
			ibctmtypes.SetIterationKey(clientStore, incrementedClientHeight)

			clientState.LatestHeight = incrementedClientHeight
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), clientID, clientState)

			updateHeader = createFutureUpdateFn(trustedHeight)
		}, true, true},
		{"client state not found", func() {
			updateHeader = createFutureUpdateFn(path.EndpointA.GetClientState().GetLatestHeight().(types.Height))

			path.EndpointA.ClientID = ibctesting.InvalidID
		}, false, false},
		{"consensus state not found", func() {
			clientState := path.EndpointA.GetClientState()
			tmClient, ok := clientState.(*ibctmtypes.ClientState)
			suite.Require().True(ok)
			tmClient.LatestHeight = tmClient.LatestHeight.Increment().(types.Height)

			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID, clientState)
			updateHeader = createFutureUpdateFn(clientState.GetLatestHeight().(types.Height))
		}, false, false},
		{"client is not active", func() {
			clientState := path.EndpointA.GetClientState().(*ibctmtypes.ClientState)
			clientState.FrozenHeight = types.NewHeight(0, 1)
			suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID, clientState)
			updateHeader = createFutureUpdateFn(clientState.GetLatestHeight().(types.Height))
		}, false, false},
		{"invalid header", func() {
			updateHeader = createFutureUpdateFn(path.EndpointA.GetClientState().GetLatestHeight().(types.Height))
			updateHeader.TrustedHeight = updateHeader.TrustedHeight.Increment().(types.Height)
		}, false, false},
	}

	for _, tc := range cases {
		tc := tc
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest()
			path = ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupClients(path)

			tc.malleate()

			var clientState exported.ClientState
			if tc.expPass {
				clientState = path.EndpointA.GetClientState()
			}

			err := suite.chainA.App.GetIBCKeeper().ClientKeeper.UpdateClient(suite.chainA.GetContext(), path.EndpointA.ClientID, updateHeader)

			if tc.expPass {
				suite.Require().NoError(err, err)

				newClientState := path.EndpointA.GetClientState()

				if tc.expFreeze {
					suite.Require().True(!newClientState.(*ibctmtypes.ClientState).FrozenHeight.IsZero(), "client did not freeze after conflicting header was submitted to UpdateClient")
				} else {
					expConsensusState := &ibctmtypes.ConsensusState{
						Timestamp:          updateHeader.GetTime(),
						Root:               commitmenttypes.NewMerkleRoot(updateHeader.Header.GetAppHash()),
						NextValidatorsHash: updateHeader.Header.NextValidatorsHash,
					}

					consensusState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientConsensusState(suite.chainA.GetContext(), path.EndpointA.ClientID, updateHeader.GetHeight())
					suite.Require().True(found)

					// Determine if clientState should be updated or not
					if updateHeader.GetHeight().GT(clientState.GetLatestHeight()) {
						// Header Height is greater than clientState latest Height, clientState should be updated with header.GetHeight()
						suite.Require().Equal(updateHeader.GetHeight(), newClientState.GetLatestHeight(), "clientstate height did not update")
					} else {
						// Update will add past consensus state, clientState should not be updated at all
						suite.Require().Equal(clientState.GetLatestHeight(), newClientState.GetLatestHeight(), "client state height updated for past header")
					}

					suite.Require().NoError(err)
					suite.Require().Equal(expConsensusState, consensusState, "consensus state should have been updated on case %s", tc.name)
				}
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestUpdateClientLocalhost() {
	revision := types.ParseChainID(suite.chainA.ChainID)
	var localhostClient exported.ClientState = localhosttypes.NewClientState(suite.chainA.ChainID, types.NewHeight(revision, uint64(suite.chainA.GetContext().BlockHeight())))

	ctx := suite.chainA.GetContext().WithBlockHeight(suite.chainA.GetContext().BlockHeight() + 1)
	err := suite.chainA.App.GetIBCKeeper().ClientKeeper.UpdateClient(ctx, exported.Localhost, nil)
	suite.Require().NoError(err)

	clientState, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientState(ctx, exported.Localhost)
	suite.Require().True(found)
	suite.Require().Equal(localhostClient.GetLatestHeight().(types.Height).Increment(), clientState.GetLatestHeight())
}

func (suite *KeeperTestSuite) TestUpgradeClient() {
	var (
		path                                        *ibctesting.Path
		upgradedClient                              exported.ClientState
		upgradedConsState                           exported.ConsensusState
		lastHeight                                  exported.Height
		proofUpgradedClient, proofUpgradedConsState []byte
		upgradedClientBz, upgradedConsStateBz       []byte
		err                                         error
	)

	testCases := []struct {
		name    string
		setup   func()
		expPass bool
	}{
		{
			name: "successful upgrade",
			setup: func() {
				// last Height is at next block
				lastHeight = clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()+1))

				// zero custom fields and store in upgrade store
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedClient(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedClientBz)
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedConsensusState(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedConsStateBz)

				// commit upgrade store changes and update clients

				suite.coordinator.CommitBlock(suite.chainB)
				err := path.EndpointA.UpdateClient()
				suite.Require().NoError(err)

				cs, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID)
				suite.Require().True(found)

				proofUpgradedClient, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedClientKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
				proofUpgradedConsState, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedConsStateKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
			},
			expPass: true,
		},
		{
			name: "client state not found",
			setup: func() {
				// last Height is at next block
				lastHeight = clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()+1))

				// zero custom fields and store in upgrade store
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedClient(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedClientBz)
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedConsensusState(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedConsStateBz)

				// commit upgrade store changes and update clients

				suite.coordinator.CommitBlock(suite.chainB)
				err := path.EndpointA.UpdateClient()
				suite.Require().NoError(err)

				cs, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID)
				suite.Require().True(found)

				proofUpgradedClient, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedClientKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
				proofUpgradedConsState, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedConsStateKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())

				path.EndpointA.ClientID = "wrongclientid"
			},
			expPass: false,
		},
		{
			name: "client state is not active",
			setup: func() {
				// client is frozen

				// last Height is at next block
				lastHeight = clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()+1))

				// zero custom fields and store in upgrade store
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedClient(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedClientBz)
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedConsensusState(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedConsStateBz)

				// commit upgrade store changes and update clients

				suite.coordinator.CommitBlock(suite.chainB)
				err := path.EndpointA.UpdateClient()
				suite.Require().NoError(err)

				cs, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID)
				suite.Require().True(found)

				proofUpgradedClient, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedClientKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
				proofUpgradedConsState, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedConsStateKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())

				// set frozen client in store
				tmClient, ok := cs.(*ibctmtypes.ClientState)
				suite.Require().True(ok)
				tmClient.FrozenHeight = types.NewHeight(0, 1)
				suite.chainA.App.GetIBCKeeper().ClientKeeper.SetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID, tmClient)
			},
			expPass: false,
		},
		{
			name: "tendermint client VerifyUpgrade fails",
			setup: func() {
				// last Height is at next block
				lastHeight = clienttypes.NewHeight(0, uint64(suite.chainB.GetContext().BlockHeight()+1))

				// zero custom fields and store in upgrade store
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedClient(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedClientBz)
				suite.chainB.GetSimApp().UpgradeKeeper.SetUpgradedConsensusState(suite.chainB.GetContext(), int64(lastHeight.GetRevisionHeight()), upgradedConsStateBz)

				// change upgradedClient client-specified parameters
				upgradedClient = ibctmtypes.NewClientState("wrongchainID", ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, newClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, true, true)

				suite.coordinator.CommitBlock(suite.chainB)
				err := path.EndpointA.UpdateClient()
				suite.Require().NoError(err)

				cs, found := suite.chainA.App.GetIBCKeeper().ClientKeeper.GetClientState(suite.chainA.GetContext(), path.EndpointA.ClientID)
				suite.Require().True(found)

				proofUpgradedClient, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedClientKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
				proofUpgradedConsState, _ = suite.chainB.QueryUpgradeProof(upgradetypes.UpgradedConsStateKey(int64(lastHeight.GetRevisionHeight())), cs.GetLatestHeight().GetRevisionHeight())
			},
			expPass: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		path = ibctesting.NewPath(suite.chainA, suite.chainB)
		suite.coordinator.SetupClients(path)
		upgradedClient = ibctmtypes.NewClientState("newChainId-1", ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod+trustingPeriod, maxClockDrift, newClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
		upgradedClient = upgradedClient.ZeroCustomFields()
		upgradedClientBz, err = types.MarshalClientState(suite.chainA.App.AppCodec(), upgradedClient)
		suite.Require().NoError(err)

		upgradedConsState = &ibctmtypes.ConsensusState{
			NextValidatorsHash: []byte("nextValsHash"),
		}
		upgradedConsStateBz, err = types.MarshalConsensusState(suite.chainA.App.AppCodec(), upgradedConsState)
		suite.Require().NoError(err)

		tc.setup()

		// Call ZeroCustomFields on upgraded clients to clear any client-chosen parameters in test-case upgradedClient
		upgradedClient = upgradedClient.ZeroCustomFields()

		err = suite.chainA.App.GetIBCKeeper().ClientKeeper.UpgradeClient(suite.chainA.GetContext(), path.EndpointA.ClientID, upgradedClient, upgradedConsState, proofUpgradedClient, proofUpgradedConsState)

		if tc.expPass {
			suite.Require().NoError(err, "verify upgrade failed on valid case: %s", tc.name)
		} else {
			suite.Require().Error(err, "verify upgrade passed on invalid case: %s", tc.name)
		}
	}

}

func (suite *KeeperTestSuite) TestCheckMisbehaviourAndUpdateState() {
	var (
		clientID string
		err      error
	)

	altPrivVal := ibctestingmock.NewPV()
	altPubKey, err := altPrivVal.GetPubKey()
	suite.Require().NoError(err)
	altVal := tmtypes.NewValidator(altPubKey, 4)

	// Set valSet here with suite.valSet so it doesn't get reset on each testcase
	valSet := suite.valSet
	valsHash := valSet.Hash()

	// Create bothValSet with both suite validator and altVal
	bothValSet := tmtypes.NewValidatorSet(append(suite.valSet.Validators, altVal))
	bothValsHash := bothValSet.Hash()
	// Create alternative validator set with only altVal
	altValSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{altVal})

	// Create signer array and ensure it is in same order as bothValSet
	_, suiteVal := suite.valSet.GetByIndex(0)
	bothSigners := ibctesting.CreateSortedSignerArray(altPrivVal, suite.privVal, altVal, suiteVal)

	altSigners := []tmtypes.PrivValidator{altPrivVal}

	// Create valid Misbehaviour by making a duplicate header that signs over different block time
	altTime := suite.ctx.BlockTime().Add(time.Minute)

	heightPlus3 := types.NewHeight(0, height+3)
	heightPlus5 := types.NewHeight(0, height+5)

	testCases := []struct {
		name         string
		misbehaviour *ibctmtypes.Misbehaviour
		malleate     func() error
		expPass      bool
	}{
		{
			"trusting period misbehavior should pass",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, altTime, bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = bothValsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				return err
			},
			true,
		},
		{
			"time misbehavior should pass",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+5), testClientHeight, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, altTime, bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = bothValsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				return err
			},
			true,
		},
		{
			"misbehavior at later height should pass",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), testClientHeight, altTime, bothValSet, valSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), testClientHeight, suite.ctx.BlockTime(), bothValSet, valSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = valsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				// store intermediate consensus state to check that trustedHeight does not need to be highest consensus state before header height
				intermediateConsState := &ibctmtypes.ConsensusState{
					Timestamp:          suite.now.Add(time.Minute),
					NextValidatorsHash: suite.chainB.Vals.Hash(),
				}
				suite.keeper.SetClientConsensusState(suite.ctx, clientID, heightPlus3, intermediateConsState)

				clientState.LatestHeight = heightPlus3
				suite.keeper.SetClientState(suite.ctx, clientID, clientState)

				return err
			},
			true,
		},
		{
			"misbehavior at later height with different trusted heights should pass",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), testClientHeight, altTime, bothValSet, valSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), heightPlus3, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = valsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				// store trusted consensus state for Header2
				intermediateConsState := &ibctmtypes.ConsensusState{
					Timestamp:          suite.now.Add(time.Minute),
					NextValidatorsHash: bothValsHash,
				}
				suite.keeper.SetClientConsensusState(suite.ctx, clientID, heightPlus3, intermediateConsState)

				clientState.LatestHeight = heightPlus3
				suite.keeper.SetClientState(suite.ctx, clientID, clientState)

				return err
			},
			true,
		},
		{
			"misbehavior ValidateBasic fails: misbehaviour height is at same height as trusted height",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight), testClientHeight, altTime, bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight), testClientHeight, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = bothValsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				return err
			},
			false,
		},
		{
			"trusted ConsensusState1 not found",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), heightPlus3, altTime, bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), testClientHeight, suite.ctx.BlockTime(), bothValSet, valSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = valsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)
				// intermediate consensus state at height + 3 is not created
				return err
			},
			false,
		},
		{
			"trusted ConsensusState2 not found",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), testClientHeight, altTime, bothValSet, valSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(heightPlus5.RevisionHeight+1), heightPlus3, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = valsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)
				// intermediate consensus state at height + 3 is not created
				return err
			},
			false,
		},
		{
			"client state not found",
			&ibctmtypes.Misbehaviour{},
			func() error { return nil },
			false,
		},
		{
			"client already is not active - client is frozen",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, altTime, bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, suite.ctx.BlockTime(), bothValSet, bothValSet, bothSigners),
				ClientId: clientID,
			},
			func() error {
				suite.consensusState.NextValidatorsHash = bothValsHash
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				clientState.FrozenHeight = types.NewHeight(0, 1)
				suite.keeper.SetClientState(suite.ctx, clientID, clientState)

				return err
			},
			false,
		},
		{
			"misbehaviour check failed",
			&ibctmtypes.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, altTime, bothValSet, bothValSet, bothSigners),
				Header2:  suite.chainA.CreateTMClientHeader(testChainID, int64(testClientHeight.RevisionHeight+1), testClientHeight, suite.ctx.BlockTime(), altValSet, bothValSet, altSigners),
				ClientId: clientID,
			},
			func() error {
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				if err != nil {
					return err
				}
				clientID, err = suite.keeper.CreateClient(suite.ctx, clientState, suite.consensusState)

				return err
			},
			false,
		},
	}

	for i, tc := range testCases {
		tc := tc
		i := i

		suite.Run(tc.name, func() {
			suite.SetupTest()       // reset
			clientID = testClientID // must be explicitly changed

			err := tc.malleate()
			suite.Require().NoError(err)

			tc.misbehaviour.ClientId = clientID

			err = suite.keeper.CheckMisbehaviourAndUpdateState(suite.ctx, tc.misbehaviour)

			if tc.expPass {
				suite.Require().NoError(err, "valid test case %d failed: %s", i, tc.name)

				clientState, found := suite.keeper.GetClientState(suite.ctx, clientID)
				suite.Require().True(found, "valid test case %d failed: %s", i, tc.name)
				suite.Require().True(!clientState.(*ibctmtypes.ClientState).FrozenHeight.IsZero(), "valid test case %d failed: %s", i, tc.name)
			} else {
				suite.Require().Error(err, "invalid test case %d passed: %s", i, tc.name)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestUpdateClientEventEmission() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path)
	header, err := suite.chainA.ConstructUpdateTMClientHeader(suite.chainB, path.EndpointA.ClientID)
	suite.Require().NoError(err)

	msg, err := clienttypes.NewMsgUpdateClient(
		path.EndpointA.ClientID, header,
		suite.chainA.SenderAccount.GetAddress().String(),
	)

	result, err := suite.chainA.SendMsgs(msg)
	suite.Require().NoError(err)
	// first event type is "message", followed by 3 "tx" events in ante
	updateEvent := result.Events[4]
	suite.Require().Equal(clienttypes.EventTypeUpdateClient, updateEvent.Type)

	// use a boolean to ensure the update event contains the header
	contains := false
	for _, attr := range updateEvent.Attributes {
		if string(attr.Key) == clienttypes.AttributeKeyHeader {
			contains = true

			bz, err := hex.DecodeString(string(attr.Value))
			suite.Require().NoError(err)

			emittedHeader, err := types.UnmarshalHeader(suite.chainA.App.AppCodec(), bz)
			suite.Require().NoError(err)
			suite.Require().Equal(header, emittedHeader)
		}

	}
	suite.Require().True(contains)

}
