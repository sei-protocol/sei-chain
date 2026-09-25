package retiredvesting

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// RegisterInterfaces registers MsgCreateVestingAccount as an sdk.Msg implementation.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgCreateVestingAccount{})
}

// amino must not register a name for MsgCreateVestingAccount: a registered name
// wraps the JSON GetSignBytes returns in a type envelope, changing the bytes
// the message was signed over.
var amino = codec.NewLegacyAmino()
