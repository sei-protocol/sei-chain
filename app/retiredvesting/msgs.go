// Package retiredvesting provides decode-only compatibility for retired vesting transactions.
package retiredvesting

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const (
	ModuleName                  = "vesting"
	TypeMsgCreateVestingAccount = "msg_create_vesting_account"
)

var _ sdk.Msg = (*MsgCreateVestingAccount)(nil)

func (msg MsgCreateVestingAccount) Route() string { return ModuleName }

func (msg MsgCreateVestingAccount) Type() string { return TypeMsgCreateVestingAccount }

func (msg MsgCreateVestingAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
}

func (msg MsgCreateVestingAccount) GetSigners() []sdk.AccAddress {
	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{from}
}

func (msg MsgCreateVestingAccount) ValidateBasic() error { return ErrDeprecated }
