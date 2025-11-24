package v100_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/suite"
	tmtypes "github.com/tendermint/tendermint/types"

	ibcclient "github.com/cosmos/ibc-go/v3/modules/core/02-client"
	clientv100 "github.com/cosmos/ibc-go/v3/modules/core/02-client/legacy/v100"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/legacy/v100"
	"github.com/cosmos/ibc-go/v3/modules/core/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
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
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	// commit some blocks so that QueryProof returns valid proof (cannot return valid query if height <= 1)
	suite.coordinator.CommitNBlocks(suite.chainA, 2)
	suite.coordinator.CommitNBlocks(suite.chainB, 2)
}

// NOTE: this test is mainly copied from 02-client/legacy/v100
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
	// NOTE: only 1 set of metadata is created, we aren't testing ordering
	// The purpose of this test is to ensure the genesis states can be marshalled/unmarshalled
	suite.coordinator.SetupClients(path)
	clientGenState := ibcclient.ExportGenesis(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper)

	// manually generate old proto buf definitions and set in genesis
	// NOTE: we cannot use 'ExportGenesis' for the solo machines since we are
	// using client states and consensus states which do not implement the exported.ClientState
	// and exported.ConsensusState interface
	var clients []clienttypes.IdentifiedClientState
	for _, sm := range []*ibctesting.Solomachine{solomachine, solomachineMulti} {
		clientState := sm.ClientState()

		var seq uint64
		if clientState.IsFrozen {
			seq = 1
		}

		// generate old client state proto defintion
		legacyClientState := &clientv100.ClientState{
			Sequence:       clientState.Sequence,
			FrozenSequence: seq,
			ConsensusState: &clientv100.ConsensusState{
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
		client := clienttypes.IdentifiedClientState{
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
		height1 := clienttypes.NewHeight(0, 1)
		height2 := clienttypes.NewHeight(1, 2)
		height3 := clienttypes.NewHeight(0, 123)

		any, err = codectypes.NewAnyWithValue(legacyClientState.ConsensusState)
		suite.Require().NoError(err)
		suite.Require().NotNil(any)
		consensusState1 := clienttypes.ConsensusStateWithHeight{
			Height:         height1,
			ConsensusState: any,
		}
		consensusState2 := clienttypes.ConsensusStateWithHeight{
			Height:         height2,
			ConsensusState: any,
		}
		consensusState3 := clienttypes.ConsensusStateWithHeight{
			Height:         height3,
			ConsensusState: any,
		}

		clientConsensusState := clienttypes.ClientConsensusStates{
			ClientId:        sm.ClientID,
			ConsensusStates: []clienttypes.ConsensusStateWithHeight{consensusState1, consensusState2, consensusState3},
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
	err := clientv100.MigrateStore(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.GetSimApp().GetKey(host.StoreKey), path.EndpointA.Chain.App.AppCodec())
	suite.Require().NoError(err)
	expectedClientGenState := ibcclient.ExportGenesis(path.EndpointA.Chain.GetContext(), path.EndpointA.Chain.App.GetIBCKeeper().ClientKeeper)

	// NOTE: these lines are added in comparison to 02-client/legacy/v100
	// generate appState with old ibc genesis state
	appState := genutiltypes.AppMap{}
	ibcGenState := types.DefaultGenesisState()
	ibcGenState.ClientGenesis = clientGenState
	clientv100.RegisterInterfaces(clientCtx.InterfaceRegistry)
	appState[host.ModuleName] = clientCtx.JSONCodec.MustMarshalJSON(ibcGenState)
	genDoc := tmtypes.GenesisDoc{
		ChainID:       suite.chainA.ChainID,
		GenesisTime:   suite.coordinator.CurrentTime,
		InitialHeight: suite.chainA.GetContext().BlockHeight(),
	}

	// NOTE: genesis time isn't updated since we aren't testing for tendermint consensus state pruning
	migrated, err := v100.MigrateGenesis(appState, clientCtx, genDoc, uint64(connectiontypes.DefaultTimePerBlock))
	suite.Require().NoError(err)

	expectedAppState := genutiltypes.AppMap{}
	expectedIBCGenState := types.DefaultGenesisState()
	expectedIBCGenState.ClientGenesis = expectedClientGenState

	bz, err := clientCtx.JSONCodec.MarshalJSON(expectedIBCGenState)
	suite.Require().NoError(err)
	expectedAppState[host.ModuleName] = bz

	suite.Require().Equal(expectedAppState, migrated)
}
