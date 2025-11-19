package testutil

import (
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*seitypes.Msg)(nil),
		&MsgCounter{},
		&MsgCounter2{},
		&MsgKeyValue{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Counter_serviceDesc)
	msgservice.RegisterMsgServiceDesc(registry, &_Counter2_serviceDesc)
	msgservice.RegisterMsgServiceDesc(registry, &_KeyValue_serviceDesc)
}

var _ seitypes.Msg = &MsgCounter{}

func (msg *MsgCounter) GetSigners() []seitypes.AccAddress { return []seitypes.AccAddress{} }
func (msg *MsgCounter) ValidateBasic() error {
	if msg.Counter >= 0 {
		return nil
	}
	return sdkerrors.Wrap(sdkerrors.ErrInvalidSequence, "counter should be a non-negative integer")
}

var _ seitypes.Msg = &MsgCounter2{}

func (msg *MsgCounter2) GetSigners() []seitypes.AccAddress { return []seitypes.AccAddress{} }
func (msg *MsgCounter2) ValidateBasic() error {
	if msg.Counter >= 0 {
		return nil
	}
	return sdkerrors.Wrap(sdkerrors.ErrInvalidSequence, "counter should be a non-negative integer")
}

var _ seitypes.Msg = &MsgKeyValue{}

func (msg *MsgKeyValue) GetSigners() []seitypes.AccAddress {
	if msg.Signer == "" {
		return []seitypes.AccAddress{}
	}

	return []seitypes.AccAddress{seitypes.MustAccAddressFromBech32(msg.Signer)}
}

func (msg *MsgKeyValue) ValidateBasic() error {
	if msg.Key == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "key cannot be nil")
	}
	if msg.Value == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "value cannot be nil")
	}
	return nil
}
