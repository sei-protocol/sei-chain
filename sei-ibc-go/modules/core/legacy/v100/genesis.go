package v100

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	clientv100 "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/legacy/v100"
	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

// MigrateGenesis accepts exported v1.0.0 IBC client genesis file and migrates it to:
//
// - Update solo machine client state protobuf definition (v1 to v2)
// - Remove all solo machine consensus states
// - Remove all expired tendermint consensus states
func MigrateGenesis(appState genutiltypes.AppMap, clientCtx client.Context, genDoc tmtypes.GenesisDoc, maxExpectedTimePerBlock uint64) (genutiltypes.AppMap, error) {
	if appState[host.ModuleName] != nil {
		// ensure legacy solo machines are registered
		clientv100.RegisterInterfaces(clientCtx.InterfaceRegistry)

		// unmarshal relative source genesis application state
		ibcGenState := &types.GenesisState{}
		clientCtx.Codec.MustUnmarshalJSON(appState[host.ModuleName], ibcGenState)

		if genDoc.InitialHeight < 0 {
			return nil, fmt.Errorf("initial height cannot be less than zero: %d", genDoc.InitialHeight)
		}

		clientGenState, err := clientv100.MigrateGenesis(codec.NewProtoCodec(clientCtx.InterfaceRegistry), &ibcGenState.ClientGenesis, genDoc.GenesisTime, clienttypes.NewHeight(clienttypes.ParseChainID(genDoc.ChainID), uint64(genDoc.InitialHeight))) // #nosec G115 --- checked above
		if err != nil {
			return nil, err
		}

		ibcGenState.ClientGenesis = *clientGenState

		// set max expected time per block
		connectionGenesis := connectiontypes.GenesisState{
			Connections:            ibcGenState.ConnectionGenesis.Connections,
			ClientConnectionPaths:  ibcGenState.ConnectionGenesis.ClientConnectionPaths,
			NextConnectionSequence: ibcGenState.ConnectionGenesis.NextConnectionSequence,
			Params:                 connectiontypes.NewParams(maxExpectedTimePerBlock),
		}

		ibcGenState.ConnectionGenesis = connectionGenesis

		// delete old genesis state
		delete(appState, host.ModuleName)

		// set new ibc genesis state
		appState[host.ModuleName] = clientCtx.Codec.MustMarshalJSON(ibcGenState)
	}
	return appState, nil
}
