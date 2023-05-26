package v100_test

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	ibcclient "github.com/cosmos/ibc-go/v3/modules/core/02-client"
	v100 "github.com/cosmos/ibc-go/v3/modules/core/02-client/legacy/v100"
	"github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

func (suite *LegacyTestSuite) TestMigrateGenesisSolomachine() {
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	encodingConfig := simapp.MakeTestEncodingConfig()
	clientCtx := client.Context{}.
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithJSONCodec(encodingConfig.Marshaler)

	// create multiple legacy solo machine clients
	solomachine := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "06-solomachine-0", "testing", 1)
	solomachineMulti := ibctesting.NewSolomachine(suite.T(), suite.chainA.Codec, "06-solomachine-1", "testing", 4)

	// create tendermint clients
	suite.coordinator.SetupClients(path)
	err := path.EndpointA.UpdateClient()
	suite.Require().NoError(err)
	clientGenState := ibcclient.ExportGenesis(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper)

	// manually generate old proto buf definitions and set in genesis
	// NOTE: we cannot use 'ExportGenesis' for the solo machines since we are
	// using client states and consensus states which do not implement the exported.ClientState
	// and exported.ConsensusState interface
	var clients []types.IdentifiedClientState
	for _, sm := range []*ibctesting.Solomachine{solomachine, solomachineMulti} {
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
		any, err := codectypes.NewAnyWithValue(legacyClientState)
		suite.Require().NoError(err)
		suite.Require().NotNil(any)
		client := types.IdentifiedClientState{
			ClientId:    sm.ClientID,
			ClientState: any,
		}
		clients = append(clients, client)

		// set in store for ease of determining expected genesis
		clientStore := path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper.ClientStore(path.EndpointA.Chain.GetContext(), sm.ClientID)
		bz, err := path.EndpointA.Chain.App.AppCodec().MarshalInterface(legacyClientState)
		suite.Require().NoError(err)
		clientStore.Set(host.ClientStateKey(), bz)

		// set some consensus states
		height1 := types.NewHeight(0, 1)
		height2 := types.NewHeight(1, 2)
		height3 := types.NewHeight(0, 123)

		any, err = codectypes.NewAnyWithValue(legacyClientState.ConsensusState)
		suite.Require().NoError(err)
		suite.Require().NotNil(any)
		consensusState1 := types.ConsensusStateWithHeight{
			Height:         height1,
			ConsensusState: any,
		}
		consensusState2 := types.ConsensusStateWithHeight{
			Height:         height2,
			ConsensusState: any,
		}
		consensusState3 := types.ConsensusStateWithHeight{
			Height:         height3,
			ConsensusState: any,
		}

		clientConsensusState := types.ClientConsensusStates{
			ClientId:        sm.ClientID,
			ConsensusStates: []types.ConsensusStateWithHeight{consensusState1, consensusState2, consensusState3},
		}

		clientGenState.ClientsConsensus = append(clientGenState.ClientsConsensus, clientConsensusState)

		// set in store for ease of determining expected genesis
		bz, err = path.EndpointA.Chain.App.AppCodec().MarshalInterface(legacyClientState.ConsensusState)
		suite.Require().NoError(err)
		clientStore.Set(host.ConsensusStateKey(height1), bz)
		clientStore.Set(host.ConsensusStateKey(height2), bz)
		clientStore.Set(host.ConsensusStateKey(height3), bz)
	}
	// solo machine clients must come before tendermint in expected
	clientGenState.Clients = append(clients, clientGenState.Clients...)

	// migrate store get expected genesis
	// store migration and genesis migration should produce identical results
	err = v100.MigrateStore(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.GetSimApp().GetKey(host.StoreKey), path.EndpointA.Chain.App.AppCodec())
	suite.Require().NoError(err)
	expectedClientGenState := ibcclient.ExportGenesis(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper)

	// NOTE: genesis time isn't updated since we aren't testing for tendermint consensus state pruning
	migrated, err := v100.MigrateGenesis(codec.NewProtoCodec(clientCtx.InterfaceRegistry), &clientGenState, suite.coordinator.CurrentTime, types.GetSelfHeight(suite.chainA.GetContext()))
	suite.Require().NoError(err)

	// 'ExportGenesis' order metadata keys by processedheight, processedtime for all heights, then it appends all iteration keys
	// In order to match the genesis migration with export genesis (from store migrations) we must reorder the iteration keys to be last
	// This isn't ideal, but it is better than modifying the genesis migration from a previous version to match the export genesis of a new version
	// which provides no benefit except nicer testing
	for i, clientMetadata := range migrated.ClientsMetadata {
		var updatedMetadata []types.GenesisMetadata
		var iterationKeys []types.GenesisMetadata
		for _, metadata := range clientMetadata.ClientMetadata {
			if bytes.HasPrefix(metadata.Key, []byte(ibctmtypes.KeyIterateConsensusStatePrefix)) {
				iterationKeys = append(iterationKeys, metadata)
			} else {
				updatedMetadata = append(updatedMetadata, metadata)
			}
		}
		updatedMetadata = append(updatedMetadata, iterationKeys...)
		migrated.ClientsMetadata[i] = types.IdentifiedGenesisMetadata{
			ClientId:       clientMetadata.ClientId,
			ClientMetadata: updatedMetadata,
		}
	}

	bz, err := clientCtx.JSONCodec.MarshalJSON(&expectedClientGenState)
	suite.Require().NoError(err)

	// Indent the JSON bz correctly.
	var jsonObj map[string]interface{}
	err = json.Unmarshal(bz, &jsonObj)
	suite.Require().NoError(err)
	expectedIndentedBz, err := json.MarshalIndent(jsonObj, "", "\t")
	suite.Require().NoError(err)

	bz, err = clientCtx.JSONCodec.MarshalJSON(migrated)
	suite.Require().NoError(err)

	// Indent the JSON bz correctly.
	err = json.Unmarshal(bz, &jsonObj)
	suite.Require().NoError(err)
	indentedBz, err := json.MarshalIndent(jsonObj, "", "\t")
	suite.Require().NoError(err)

	suite.Require().Equal(string(expectedIndentedBz), string(indentedBz))
}

func (suite *LegacyTestSuite) TestMigrateGenesisTendermint() {
	// create two paths and setup clients
	path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
	path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
	encodingConfig := simapp.MakeTestEncodingConfig()
	clientCtx := client.Context{}.
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithJSONCodec(encodingConfig.Marshaler)

	suite.coordinator.SetupClients(path1)
	suite.coordinator.SetupClients(path2)

	// collect all heights expected to be pruned
	var path1PruneHeights, path2PruneHeights []exported.Height
	path1PruneHeights = append(path1PruneHeights, path1.EndpointA.GetClientState().GetLatestHeight())
	path2PruneHeights = append(path2PruneHeights, path2.EndpointA.GetClientState().GetLatestHeight())

	// these heights will be expired and also pruned
	for i := 0; i < 3; i++ {
		path1.EndpointA.UpdateClient()
		path1PruneHeights = append(path1PruneHeights, path1.EndpointA.GetClientState().GetLatestHeight())
	}
	for i := 0; i < 3; i++ {
		path2.EndpointA.UpdateClient()
		path2PruneHeights = append(path2PruneHeights, path2.EndpointA.GetClientState().GetLatestHeight())
	}

	// Increment the time by a week
	suite.coordinator.IncrementTimeBy(7 * 24 * time.Hour)

	// create the consensus state that can be used as trusted height for next update
	path1.EndpointA.UpdateClient()
	path1.EndpointA.UpdateClient()
	path2.EndpointA.UpdateClient()
	path2.EndpointA.UpdateClient()

	clientGenState := ibcclient.ExportGenesis(suite.chainA.GetContext(), suite.chainA.App.GetIBCKeeper().ClientKeeper)
	suite.Require().NotNil(clientGenState.Clients)
	suite.Require().NotNil(clientGenState.ClientsConsensus)
	suite.Require().NotNil(clientGenState.ClientsMetadata)

	// Increment the time by another week, then update the client.
	// This will cause the consensus states created before the first time increment
	// to be expired
	suite.coordinator.IncrementTimeBy(7 * 24 * time.Hour)

	// migrate store get expected genesis
	// store migration and genesis migration should produce identical results
	err := v100.MigrateStore(path1.EndpointA.Chain.GetContext(), path1.EndpointA.Chain.GetSimApp().GetKey(host.StoreKey), path1.EndpointA.Chain.App.AppCodec())
	suite.Require().NoError(err)
	expectedClientGenState := ibcclient.ExportGenesis(path1.EndpointA.Chain.GetContext(), path1.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper)

	migrated, err := v100.MigrateGenesis(codec.NewProtoCodec(clientCtx.InterfaceRegistry), &clientGenState, suite.coordinator.CurrentTime, types.GetSelfHeight(suite.chainA.GetContext()))
	suite.Require().NoError(err)

	// 'ExportGenesis' order metadata keys by processedheight, processedtime for all heights, then it appends all iteration keys
	// In order to match the genesis migration with export genesis we must reorder the iteration keys to be last
	// This isn't ideal, but it is better than modifying the genesis migration from a previous version to match the export genesis of a new version
	// which provides no benefit except nicer testing
	for i, clientMetadata := range migrated.ClientsMetadata {
		var updatedMetadata []types.GenesisMetadata
		var iterationKeys []types.GenesisMetadata
		for _, metadata := range clientMetadata.ClientMetadata {
			if bytes.HasPrefix(metadata.Key, []byte(ibctmtypes.KeyIterateConsensusStatePrefix)) {
				iterationKeys = append(iterationKeys, metadata)
			} else {
				updatedMetadata = append(updatedMetadata, metadata)
			}
		}
		updatedMetadata = append(updatedMetadata, iterationKeys...)
		migrated.ClientsMetadata[i] = types.IdentifiedGenesisMetadata{
			ClientId:       clientMetadata.ClientId,
			ClientMetadata: updatedMetadata,
		}
	}

	// check path 1 client pruning
	for _, height := range path1PruneHeights {
		for _, client := range migrated.ClientsConsensus {
			if client.ClientId == path1.EndpointA.ClientID {
				for _, consensusState := range client.ConsensusStates {
					suite.Require().NotEqual(height, consensusState.Height)
				}
			}

		}
		for _, client := range migrated.ClientsMetadata {
			if client.ClientId == path1.EndpointA.ClientID {
				for _, metadata := range client.ClientMetadata {
					suite.Require().NotEqual(ibctmtypes.ProcessedTimeKey(height), metadata.Key)
					suite.Require().NotEqual(ibctmtypes.ProcessedHeightKey(height), metadata.Key)
					suite.Require().NotEqual(ibctmtypes.IterationKey(height), metadata.Key)
				}
			}
		}
	}

	// check path 2 client pruning
	for _, height := range path2PruneHeights {
		for _, client := range migrated.ClientsConsensus {
			if client.ClientId == path2.EndpointA.ClientID {
				for _, consensusState := range client.ConsensusStates {
					suite.Require().NotEqual(height, consensusState.Height)
				}
			}

		}
		for _, client := range migrated.ClientsMetadata {
			if client.ClientId == path2.EndpointA.ClientID {
				for _, metadata := range client.ClientMetadata {
					suite.Require().NotEqual(ibctmtypes.ProcessedTimeKey(height), metadata.Key)
					suite.Require().NotEqual(ibctmtypes.ProcessedHeightKey(height), metadata.Key)
					suite.Require().NotEqual(ibctmtypes.IterationKey(height), metadata.Key)
				}
			}

		}
	}
	bz, err := clientCtx.JSONCodec.MarshalJSON(&expectedClientGenState)
	suite.Require().NoError(err)

	// Indent the JSON bz correctly.
	var jsonObj map[string]interface{}
	err = json.Unmarshal(bz, &jsonObj)
	suite.Require().NoError(err)
	expectedIndentedBz, err := json.MarshalIndent(jsonObj, "", "\t")
	suite.Require().NoError(err)

	bz, err = clientCtx.JSONCodec.MarshalJSON(migrated)
	suite.Require().NoError(err)

	// Indent the JSON bz correctly.
	err = json.Unmarshal(bz, &jsonObj)
	suite.Require().NoError(err)
	indentedBz, err := json.MarshalIndent(jsonObj, "", "\t")
	suite.Require().NoError(err)

	suite.Require().Equal(string(expectedIndentedBz), string(indentedBz))
}
