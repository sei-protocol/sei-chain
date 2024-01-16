package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgInternalEVMCall{}
)

func NewMessageInternalEVMCall(from sdk.AccAddress, to string, value *sdk.Int, data []byte) *MsgInternalEVMCall {
	return &MsgInternalEVMCall{
		Sender: from.String(),
		To:     to,
		Value:  value,
		Data:   data,
	}
}

func (msg *MsgInternalEVMCall) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{sdk.MustAccAddressFromBech32(msg.Sender)}
}

func (msg *MsgInternalEVMCall) ValidateBasic() error {
	return nil
}
