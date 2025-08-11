package types

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	cdctypes "github.com/sei-protocol/sei-chain/cosmos-sdk/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/cosmos-sdk/crypto/codec"
	govtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/types"
)

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterCodec(amino)
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&UpdateMinterProposal{}, "mint/UpdateMinter", nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*govtypes.Content)(nil),
		&UpdateMinterProposal{},
	)
}
