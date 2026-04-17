package app

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/std"

	"github.com/sei-protocol/sei-chain/sei-wasmd/app/params"
)

// MakeEncodingConfig creates a new EncodingConfig with all modules registered
func MakeEncodingConfig() params.EncodingConfig {
	encodingConfig := params.MakeEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}
