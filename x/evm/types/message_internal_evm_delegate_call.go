package types

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var (
	_ sdk.Msg = &MsgInternalEVMDelegateCall{}
)

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
