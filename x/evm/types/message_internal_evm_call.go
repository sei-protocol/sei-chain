package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

var (
	_ seitypes.Msg = &MsgInternalEVMCall{}
)

func NewMessageInternalEVMCall(from seitypes.AccAddress, to string, value *sdk.Int, data []byte) *MsgInternalEVMCall {
	return &MsgInternalEVMCall{
		Sender: from.String(),
		To:     to,
		Value:  value,
		Data:   data,
	}
}

func (msg *MsgInternalEVMCall) GetSigners() []seitypes.AccAddress {
	senderAddr, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return []seitypes.AccAddress{}
	}
	return []seitypes.AccAddress{senderAddr}
}

func (msg *MsgInternalEVMCall) ValidateBasic() error {
	return nil
}
