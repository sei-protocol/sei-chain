package feegrant

import (
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// RegisterInterfaces registers the interfaces types with the interface registry
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgGrantAllowance{},
		&MsgRevokeAllowance{},
	)

	registry.RegisterInterface(
		"cosmos.feegrant.v1beta1.FeeAllowanceI",
		(*FeeAllowanceI)(nil),
		&BasicAllowance{},
		&PeriodicAllowance{},
		&AllowedMsgAllowance{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
