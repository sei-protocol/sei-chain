package authz

import (
	types "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// RegisterInterfaces registers the interfaces types with the interface registry
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations((*seitypes.Msg)(nil),
		&MsgGrant{},
		&MsgRevoke{},
		&MsgExec{},
	)

	registry.RegisterInterface(
		"cosmos.v1beta1.Authorization",
		(*Authorization)(nil),
		&GenericAuthorization{},
	)

	msgservice.RegisterMsgServiceDesc(registry, MsgServiceDesc())
}
