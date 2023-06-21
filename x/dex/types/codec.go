package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	authzcodec "github.com/cosmos/cosmos-sdk/x/authz/codec"
	govcodec "github.com/cosmos/cosmos-sdk/x/gov/codec"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPlaceOrders{}, "dex/MsgPlaceOrders", nil)
	cdc.RegisterConcrete(&MsgCancelOrders{}, "dex/MsgCancelOrders", nil)
	cdc.RegisterConcrete(&MsgRegisterContract{}, "dex/MsgRegisterContract", nil)
	cdc.RegisterConcrete(&MsgRegisterPairs{}, "dex/MsgRegisterPairs", nil)
	cdc.RegisterConcrete(&MsgUpdatePriceTickSize{}, "dex/MsgUpdatePriceTickSize", nil)
	cdc.RegisterConcrete(&MsgUpdateQuantityTickSize{}, "dex/MsgUpdateQuantityTickSize", nil)
	cdc.RegisterConcrete(&AddAssetMetadataProposal{}, "dex/AddAssetMetadataProposal", nil)
	cdc.RegisterConcrete(&MsgUnregisterContract{}, "dex/MsgUnregisterContract", nil)
	cdc.RegisterConcrete(&MsgContractDepositRent{}, "dex/MsgContractDepositRent", nil)
	cdc.RegisterConcrete(&MsgUnsuspendContract{}, "dex/MsgUnsuspendContract", nil)
	// this line is used by starport scaffolding # 2
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPlaceOrders{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelOrders{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterContract{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterPairs{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdatePriceTickSize{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateQuantityTickSize{},
	)
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&AddAssetMetadataProposal{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnregisterContract{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgContractDepositRent{},
	)
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnsuspendContract{},
	)
	// this line is used by starport scaffolding # 3

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)

func init() {
	std.RegisterLegacyAminoCodec(Amino)
	cryptocodec.RegisterCrypto(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)

	// Register all Amino interfaces and concrete types on the authz  and gov Amino codec
	// so that this can later be used to properly serialize MsgGrant and MsgExec
	// instances.
	std.RegisterLegacyAminoCodec(authzcodec.Amino)
	std.RegisterLegacyAminoCodec(govcodec.Amino)

	RegisterCodec(Amino)
	cryptocodec.RegisterCrypto(Amino)
	Amino.Seal()
}
