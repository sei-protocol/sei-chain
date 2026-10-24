package retiredauthz

import (
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// RegisterInterfaces registers the retired authz messages as sdk.Msg
// implementations, and every authorization a retired grant can carry as an
// Authorization.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	// No amino names are registered, as the removed module registered none: a name
	// would wrap the JSON GetSignBytes returns in a type envelope, changing the
	// bytes historical transactions were signed over.
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgGrant{},
		&MsgExec{},
		&MsgRevoke{},
	)
	registry.RegisterInterface("cosmos.v1beta1.Authorization", (*Authorization)(nil),
		&GenericAuthorization{},
		&banktypes.SendAuthorization{},
		&stakingtypes.StakeAuthorization{},
	)
}
