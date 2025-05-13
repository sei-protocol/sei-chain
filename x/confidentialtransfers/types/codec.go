package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgTransfer{}, "confidentialtransfers/MsgTransfer", nil)
	cdc.RegisterConcrete(&MsgInitializeAccount{}, "confidentialtransfers/MsgInitializeAccount", nil)
	cdc.RegisterConcrete(&MsgDeposit{}, "confidentialtransfers/MsgDeposit", nil)
	cdc.RegisterConcrete(&MsgWithdraw{}, "confidentialtransfers/MsgWithdraw", nil)
	cdc.RegisterConcrete(&MsgCloseAccount{}, "confidentialtransfers/MsgCloseAccount", nil)
	cdc.RegisterConcrete(&MsgApplyPendingBalance{}, "confidentialtransfers/MsgApplyPendingBalance", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransfer{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgInitializeAccount{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeposit{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgWithdraw{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCloseAccount{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApplyPendingBalance{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterCodec(amino)
	sdk.RegisterLegacyAminoCodec(amino)

	amino.Seal()
}
