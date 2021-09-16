package v100_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v2/modules/core/02-client/legacy/v100"
	"github.com/cosmos/ibc-go/v2/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/v2/modules/core/24-host"
	"github.com/cosmos/ibc-go/v2/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v2/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v2/testing"
)

type LegacyTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
}

// TestLegacyTestSuite runs all the tests within this package.
func TestLegacyTestSuite(t *testing.T) {
	suite.Run(t, new(LegacyTestSuite))
}

// SetupTest creates a coordinator with 2 test chains.
func (suite *LegacyTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(0))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	// commit some blocks so that QueryProof returns valid proof (cannot return valid query if height <= 1)
	suite.coordinator.CommitNBlocks(suite.chainA, 2)
	suite.coordinator.CommitNBlocks(suite.chainB, 2)
}

// only test migration for solo machines
// ensure all client states are migrated and all consensus states
// are removed
func (suite *LegacyTestSuite) TestMigrateStoreSolomachine() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)

	// create multiple legacy solo machine clients
	solomachine := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "06-solomachine-0", "testing", 1)
	solomachineMulti := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "06-solomachine-1", "testing", 4)

	// manually generate old proto buf definitions and set in store
	// NOTE: we cannot use 'CreateClient' and 'UpdateClient' functions since we are
	// using client states and consensus states which do not implement the exported.ClientState
	// and exported.ConsensusState interface
	for _, sm := range []*ibctesting.Solomachine{solomachine, solomachineMulti} {
		clientStore := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.ClientStore(path.EndpointA.Chain.GetContext(), sm.ClientID)
		clientState := sm.ClientState()

		var seq uint64
		if clientState.IsFrozen {
			seq = 1
		}

		// generate old client state proto defintion
		legacyClientState := &v100.ClientState{
			Sequence:       clientState.Sequence,
			FrozenSequence: seq,
			ConsensusState: &v100.ConsensusState{
				PublicKey:   clientState.ConsensusState.PublicKey,
				Diversifier: clientState.ConsensusState.Diversifier,
				Timestamp:   clientState.ConsensusState.Timestamp,
			},
			AllowUpdateAfterProposal: clientState.AllowUpdateAfterProposal,
		}

		// set client state
		bz, err := path.EndpointA.Chain.App.AppCodec().MarshalInterface(legacyClientState)
		suite.Require().NoError(err)
		clientStore.Set(host.ClientStateKey(), bz)

		// set some consensus states
		height1 := types.NewHeight(0, 1)
		height2 := types.NewHeight(1, 2)
		height3 := types.NewHeight(0, 123)

		bz, err = path.EndpointA.Chain.App.AppCodec().MarshalInterface(legacyClientState.ConsensusState)
		suite.Require().NoError(err)
		clientStore.Set(host.ConsensusStateKey(height1), bz)
		clientStore.Set(host.ConsensusStateKey(height2), bz)
		clientStore.Set(host.ConsensusStateKey(height3), bz)
	}

	// create tendermint clients
	suite.coordinator.SetupClients(path)

	err := v100.MigrateStore(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.GetSimApp().GetKey(host.StoreKey), path.EndpointA.Chain.App.AppCodec())
	suite.Require().NoError(err)

	// verify client state has been migrated
	for _, sm := range []*ibctesting.Solomachine{solomachine, solomachineMulti} {
		clientState, ok := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.GetClientState(path.EndpointA.Chain.GetContext(), sm.ClientID)
		suite.Require().True(ok)
		suite.Require().Equal(sm.ClientState(), clientState)
	}

	// verify consensus states have been removed
	for _, sm := range []*ibctesting.Solomachine{solomachine, solomachineMulti} {
		clientConsensusStates := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.GetAllConsensusStates(path.EndpointA.Chain.GetContext())
		for _, client := range clientConsensusStates {
			// GetAllConsensusStates should not return consensus states for our solo machine clients
			suite.Require().NotEqual(sm.ClientID, client.ClientId)
		}
	}
}

// only test migration for tendermint clients
// ensure all expired consensus states are removed from tendermint client stores
func (suite *LegacyTestSuite) TestMigrateStoreTendermint() {
	// create path and setup clients
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path1)

	path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path2)
	pruneHeightMap := make(map[*ibctesting.Path][]exported.Height)
	unexpiredHeightMap := make(map[*ibctesting.Path][]exported.Height)

	for _, path := range []*ibctesting.Path{path1, path2} {
		// collect all heights expected to be pruned
		var pruneHeights []exported.Height
		pruneHeights = append(pruneHeights, path.EndpointA.GetClientState().GetLatestHeight())

		// these heights will be expired and also pruned
		for i := 0; i < 3; i++ {
			path.EndpointA.UpdateClient()
			pruneHeights = append(pruneHeights, path.EndpointA.GetClientState().GetLatestHeight())
		}

		// double chedck all information is currently stored
		for _, pruneHeight := range pruneHeights {
			consState, ok := path.EndpointA.Chain.GetConsensusState(path.EndpointA.ClientID, pruneHeight)
			suite.Require().True(ok)
			suite.Require().NotNil(consState)

			ctx := path.EndpointA.Chain.GetContext()
			clientStore := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.ClientStore(ctx, path.EndpointA.ClientID)

			processedTime, ok := ibctmtypes.GetProcessedTime(clientStore, pruneHeight)
			suite.Require().True(ok)
			suite.Require().NotNil(processedTime)

			processedHeight, ok := ibctmtypes.GetProcessedHeight(clientStore, pruneHeight)
			suite.Require().True(ok)
			suite.Require().NotNil(processedHeight)

			expectedConsKey := ibctmtypes.GetIterationKey(clientStore, pruneHeight)
			suite.Require().NotNil(expectedConsKey)
		}
		pruneHeightMap[path] = pruneHeights
	}

	// Increment the time by a week
	suite.coordinator.IncrementTimeBy(7 * 24 * time.Hour)

	for _, path := range []*ibctesting.Path{path1, path2} {
		// create the consensus state that can be used as trusted height for next update
		var unexpiredHeights []exported.Height
		path.EndpointA.UpdateClient()
		unexpiredHeights = append(unexpiredHeights, path.EndpointA.GetClientState().GetLatestHeight())
		path.EndpointA.UpdateClient()
		unexpiredHeights = append(unexpiredHeights, path.EndpointA.GetClientState().GetLatestHeight())

		// remove processed height and iteration keys since these were missing from previous version of ibc module
		clientStore := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.ClientStore(path.EndpointA.Chain.GetContext(), path.EndpointA.ClientID)
		for _, height := range unexpiredHeights {
			clientStore.Delete(ibctmtypes.ProcessedHeightKey(height))
			clientStore.Delete(ibctmtypes.IterationKey(height))
		}

		unexpiredHeightMap[path] = unexpiredHeights
	}

	// Increment the time by another week, then update the client.
	// This will cause the consensus states created before the first time increment
	// to be expired
	suite.coordinator.IncrementTimeBy(7 * 24 * time.Hour)
	err := v100.MigrateStore(path1.EndpointA.Chain.GetContext(), path1.EndpointA.Chain.GetSimApp().GetKey(host.StoreKey), path1.EndpointA.Chain.App.AppCodec())
	suite.Require().NoError(err)

	for _, path := range []*ibctesting.Path{path1, path2} {
		ctx := path.EndpointA.Chain.GetContext()
		clientStore := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.ClientStore(ctx, path.EndpointA.ClientID)

		// ensure everything has been pruned
		for i, pruneHeight := range pruneHeightMap[path] {
			consState, ok := path.EndpointA.Chain.GetConsensusState(path.EndpointA.ClientID, pruneHeight)
			suite.Require().False(ok, i)
			suite.Require().Nil(consState, i)

			processedTime, ok := ibctmtypes.GetProcessedTime(clientStore, pruneHeight)
			suite.Require().False(ok, i)
			suite.Require().Equal(uint64(0), processedTime, i)

			processedHeight, ok := ibctmtypes.GetProcessedHeight(clientStore, pruneHeight)
			suite.Require().False(ok, i)
			suite.Require().Nil(processedHeight, i)

			expectedConsKey := ibctmtypes.GetIterationKey(clientStore, pruneHeight)
			suite.Require().Nil(expectedConsKey, i)
		}

		// ensure metadata is set for unexpired consensus state
		for _, height := range unexpiredHeightMap[path] {
			consState, ok := path.EndpointA.Chain.GetConsensusState(path.EndpointA.ClientID, height)
			suite.Require().True(ok)
			suite.Require().NotNil(consState)

			processedTime, ok := ibctmtypes.GetProcessedTime(clientStore, height)
			suite.Require().True(ok)
			suite.Require().NotEqual(uint64(0), processedTime)

			processedHeight, ok := ibctmtypes.GetProcessedHeight(clientStore, height)
			suite.Require().True(ok)
			suite.Require().Equal(types.GetSelfHeight(path.EndpointA.Chain.GetContext()), processedHeight)

			consKey := ibctmtypes.GetIterationKey(clientStore, height)
			suite.Require().Equal(host.ConsensusStateKey(height), consKey)
		}
	}
}
