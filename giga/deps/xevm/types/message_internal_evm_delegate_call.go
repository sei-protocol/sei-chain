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
	contractAddr, err := sdk.AccAddressFromBech32(msg.FromContract)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{contractAddr}
}

func (msg *MsgInternalEVMDelegateCall) ValidateBasic() error {
	return nil
}
