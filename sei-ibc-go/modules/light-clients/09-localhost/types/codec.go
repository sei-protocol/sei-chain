package types

import (
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
)

// RegisterInterfaces register the ibc interfaces submodule implementations to protobuf
// Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*exported.ClientState)(nil),
		&ClientState{},
	)
}
