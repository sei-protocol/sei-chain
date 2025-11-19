package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ seitypes.Msg = &MsgInternalEVMDelegateCall{}
)

func NewMessageInternalEVMDelegateCall(from seitypes.AccAddress, to string, codeHash []byte, data []byte, fromContract string) *MsgInternalEVMDelegateCall {
	return &MsgInternalEVMDelegateCall{
		Sender:       from.String(),
		To:           to,
		Data:         data,
		CodeHash:     codeHash,
		FromContract: fromContract,
	}
}

func (msg *MsgInternalEVMDelegateCall) GetSigners() []seitypes.AccAddress {
	contractAddr, err := seitypes.AccAddressFromBech32(msg.FromContract)
	if err != nil {
		return []seitypes.AccAddress{}
	}
	return []seitypes.AccAddress{contractAddr}
}

func (msg *MsgInternalEVMDelegateCall) ValidateBasic() error {
	return nil
}
