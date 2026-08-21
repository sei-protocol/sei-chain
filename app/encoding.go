package app

import (
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/std"
	ibctransfertypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

// MakeEncodingConfig creates an EncodingConfig for testing.
func MakeEncodingConfig() params.EncodingConfig {
	encodingConfig := params.MakeEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	registerRetiredModuleCodecs(encodingConfig)
	return encodingConfig
}

// MakeLegacyEncodingConfig creates an EncodingConfig for testing.
func MakeLegacyEncodingConfig() params.EncodingConfig {
	encodingConfig := params.MakeLegacyEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	registerRetiredModuleCodecs(encodingConfig)
	return encodingConfig
}

// registerRetiredModuleCodecs keeps historical transactions decodable without
// registering the retired modules' commands, services, or genesis state.
func registerRetiredModuleCodecs(encodingConfig params.EncodingConfig) {
	ibctransfertypes.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ibctransfertypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)
}
