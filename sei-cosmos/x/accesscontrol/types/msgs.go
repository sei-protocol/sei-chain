package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	TypeMsgRegisterWasmDependency = "register_wasm_dependency"
)

var (
	_ sdk.Msg = &MsgRegisterWasmDependency{}
)

func NewMsgRegisterWasmDependencyFromJSON(fromAddr sdk.AccAddress, jsonFile RegisterWasmDependencyJSONFile) *MsgRegisterWasmDependency {
	m := &MsgRegisterWasmDependency{
		FromAddress:           fromAddr.String(),
		ContractAddress:       jsonFile.ContractAddress,
		WasmDependencyMapping: jsonFile.WasmDependencyMapping,
	}
	return m
}

// Route implements Msg
func (m MsgRegisterWasmDependency) Route() string { return RouterKey }

// Type implements Msg
func (m MsgRegisterWasmDependency) Type() string { return TypeMsgRegisterWasmDependency }

// ValidateBasic implements Msg
func (m MsgRegisterWasmDependency) ValidateBasic() error {
	if m.FromAddress == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, m.FromAddress)
	}

	if m.ContractAddress == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, m.ContractAddress)
	}

	return nil
}

// GetSignBytes implements Msg
func (m MsgRegisterWasmDependency) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&m)
	return sdk.MustSortJSON(bz)
}

// GetSigners implements Msg
func (m MsgRegisterWasmDependency) GetSigners() []sdk.AccAddress {
	fromAddr, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{fromAddr}
}
