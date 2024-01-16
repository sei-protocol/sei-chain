package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgInternalEVMDelegateCall{}
)

func NewMessageInternalEVMDelegateCall(from sdk.AccAddress, to string, codeHash []byte, data []byte, fromContract string) *MsgInternalEVMDelegateCall {
	return &MsgInternalEVMDelegateCall{
		Sender:       from.String(),
		To:           to,
		Data:         data,
		CodeHash:     codeHash,
		FromContract: fromContract,
	}
}

func (msg *MsgInternalEVMDelegateCall) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{sdk.MustAccAddressFromBech32(msg.FromContract)}
}

func (msg *MsgInternalEVMDelegateCall) ValidateBasic() error {
	return nil
}
