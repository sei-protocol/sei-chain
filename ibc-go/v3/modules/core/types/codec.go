package types

import (
	codectypes "github.com/sei-protocol/sei-chain/cosmos-sdk/codec/types"

	clienttypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/04-channel/types"
	commitmenttypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/23-commitment/types"
	solomachinetypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/light-clients/06-solomachine/types"
	ibctmtypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/light-clients/07-tendermint/types"
	localhosttypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/light-clients/09-localhost/types"
)

// RegisterInterfaces registers x/ibc interfaces into protobuf Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	clienttypes.RegisterInterfaces(registry)
	connectiontypes.RegisterInterfaces(registry)
	channeltypes.RegisterInterfaces(registry)
	solomachinetypes.RegisterInterfaces(registry)
	ibctmtypes.RegisterInterfaces(registry)
	localhosttypes.RegisterInterfaces(registry)
	commitmenttypes.RegisterInterfaces(registry)
}
