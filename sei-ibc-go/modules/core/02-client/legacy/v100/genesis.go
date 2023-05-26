package v100

import (
	"bytes"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
)

// MigrateGenesis accepts exported v1.0.0 IBC client genesis file and migrates it to:
//
// - Update solo machine client state protobuf definition (v1 to v2)
// - Remove all solo machine consensus states
// - Remove all expired tendermint consensus states
// - Adds ProcessedHeight and Iteration keys for unexpired tendermint consensus states
func MigrateGenesis(cdc codec.BinaryCodec, clientGenState *types.GenesisState, genesisBlockTime time.Time, selfHeight exported.Height) (*types.GenesisState, error) {
	// To prune the consensus states, we will create new clientsConsensus
	// and clientsMetadata. These slices will be filled up with consensus states
	// which should not be pruned. No solo machine consensus states should be added
	// and only unexpired consensus states for tendermint clients will be added.
	// The metadata keys for unexpired consensus states will be added to clientsMetadata
	var (
		clientsConsensus []types.ClientConsensusStates
		clientsMetadata  []types.IdentifiedGenesisMetadata
	)

	for i, client := range clientGenState.Clients {
		clientType, _, err := types.ParseClientIdentifier(client.ClientId)
		if err != nil {
			return nil, err
		}

		// update solo machine client state defintions
		if clientType == exported.Solomachine {
			clientState := &ClientState{}
			if err := cdc.Unmarshal(client.ClientState.Value, clientState); err != nil {
				return nil, sdkerrors.Wrap(err, "failed to unmarshal client state bytes into solo machine client state")
			}

			updatedClientState := migrateSolomachine(clientState)

			any, err := types.PackClientState(updatedClientState)
			if err != nil {
				return nil, err
			}

			clientGenState.Clients[i] = types.IdentifiedClientState{
				ClientId:    client.ClientId,
				ClientState: any,
			}
		}

		// iterate consensus states by client
		for _, clientConsensusStates := range clientGenState.ClientsConsensus {
			// look for consensus states for the current client
			if clientConsensusStates.ClientId == client.ClientId {
				switch clientType {
				case exported.Solomachine:
					// remove all consensus states for the solo machine
					// do not add to new clientsConsensus

				case exported.Tendermint:
					// only add non expired consensus states to new clientsConsensus
					tmClientState, ok := client.ClientState.GetCachedValue().(*ibctmtypes.ClientState)
					if !ok {
						return nil, types.ErrInvalidClient
					}

					// collect unexpired consensus states
					var unexpiredConsensusStates []types.ConsensusStateWithHeight
					for _, consState := range clientConsensusStates.ConsensusStates {
						tmConsState := consState.ConsensusState.GetCachedValue().(*ibctmtypes.ConsensusState)
						if !tmClientState.IsExpired(tmConsState.Timestamp, genesisBlockTime) {
							unexpiredConsensusStates = append(unexpiredConsensusStates, consState)
						}
					}

					// if we found at least one unexpired consensus state, create a clientConsensusState
					// and add it to clientsConsensus
					if len(unexpiredConsensusStates) != 0 {
						clientsConsensus = append(clientsConsensus, types.ClientConsensusStates{
							ClientId:        client.ClientId,
							ConsensusStates: unexpiredConsensusStates,
						})
					}

					// collect metadata for unexpired consensus states
					var clientMetadata []types.GenesisMetadata

					// remove all expired tendermint consensus state metadata by adding only
					// unexpired consensus state metadata
					for _, consState := range unexpiredConsensusStates {
						for _, identifiedGenMetadata := range clientGenState.ClientsMetadata {
							// look for metadata for current client
							if identifiedGenMetadata.ClientId == client.ClientId {

								// obtain height for consensus state being pruned
								height := consState.Height

								// iterate through metadata and find metadata for current unexpired height
								// only unexpired consensus state metadata should be added
								for _, metadata := range identifiedGenMetadata.ClientMetadata {
									// the previous version of IBC only contained the processed time metadata
									// if we find the processed time metadata for an unexpired height, add the
									// iteration key and processed height keys.
									if bytes.Equal(metadata.Key, ibctmtypes.ProcessedTimeKey(height)) {
										clientMetadata = append(clientMetadata,
											// set the processed height using the current self height
											// this is safe, it may cause delays in packet processing if there
											// is a non zero connection delay time
											types.GenesisMetadata{
												Key:   ibctmtypes.ProcessedHeightKey(height),
												Value: []byte(selfHeight.String()),
											},
											metadata, // processed time
											types.GenesisMetadata{
												Key:   ibctmtypes.IterationKey(height),
												Value: host.ConsensusStateKey(height),
											})

									}
								}

							}
						}

					}

					// if we have metadata for unexipred consensus states, add it to consensusMetadata
					if len(clientMetadata) != 0 {
						clientsMetadata = append(clientsMetadata, types.IdentifiedGenesisMetadata{
							ClientId:       client.ClientId,
							ClientMetadata: clientMetadata,
						})
					}

				default:
					break
				}
			}
		}
	}

	clientGenState.ClientsConsensus = clientsConsensus
	clientGenState.ClientsMetadata = clientsMetadata
	return clientGenState, nil
}
