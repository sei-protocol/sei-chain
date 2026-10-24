// Package retiredauthz provides decode-only compatibility for retired authz transactions.
package retiredauthz

import (
	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/legacy"
	cdctypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// ModuleName is the name of the retired authz module, which also names its
// deleted store and its error codespace.
const ModuleName = "authz"

var (
	_ sdk.Msg = (*MsgGrant)(nil)
	_ sdk.Msg = (*MsgExec)(nil)
	_ sdk.Msg = (*MsgRevoke)(nil)

	_ cdctypes.UnpackInterfacesMessage = (*MsgGrant)(nil)
	_ cdctypes.UnpackInterfacesMessage = (*MsgExec)(nil)
	_ cdctypes.UnpackInterfacesMessage = (*Grant)(nil)
)

// Authorization is the interface the authorization in a retired grant is
// registered and unpacked under.
type Authorization interface {
	proto.Message
}

func (msg MsgGrant) Route() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgGrant) Type() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgGrant) GetSignBytes() []byte {
	return sdk.MustSortJSON(legacy.Cdc.MustMarshalJSON(&msg))
}

func (msg MsgGrant) GetSigners() []sdk.AccAddress { return mustSigner(msg.Granter) }

func (msg MsgGrant) ValidateBasic() error { return ErrDeprecated }

func (msg MsgGrant) UnpackInterfaces(unpacker cdctypes.AnyUnpacker) error {
	return msg.Grant.UnpackInterfaces(unpacker)
}

func (g Grant) UnpackInterfaces(unpacker cdctypes.AnyUnpacker) error {
	var authorization Authorization
	return unpacker.UnpackAny(g.Authorization, &authorization)
}

func (msg MsgExec) Route() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgExec) Type() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgExec) GetSignBytes() []byte {
	return sdk.MustSortJSON(legacy.Cdc.MustMarshalJSON(&msg))
}

func (msg MsgExec) GetSigners() []sdk.AccAddress { return mustSigner(msg.Grantee) }

func (msg MsgExec) ValidateBasic() error { return ErrDeprecated }

func (msg MsgExec) UnpackInterfaces(unpacker cdctypes.AnyUnpacker) error {
	for _, m := range msg.Msgs {
		var authorized sdk.Msg
		if err := unpacker.UnpackAny(m, &authorized); err != nil {
			return err
		}
	}
	return nil
}

func (msg MsgRevoke) Route() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgRevoke) Type() string { return sdk.MsgTypeURL(&msg) }

func (msg MsgRevoke) GetSignBytes() []byte {
	return sdk.MustSortJSON(legacy.Cdc.MustMarshalJSON(&msg))
}

func (msg MsgRevoke) GetSigners() []sdk.AccAddress { return mustSigner(msg.Granter) }

func (msg MsgRevoke) ValidateBasic() error { return ErrDeprecated }

// mustSigner returns address as the only signer, and panics when it does not parse.
func mustSigner(address string) []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
}
