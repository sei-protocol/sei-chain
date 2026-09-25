package app

import (
	"github.com/sei-protocol/sei-chain/app/params"
	retiredibcgov "github.com/sei-protocol/sei-chain/app/retiredibc/gov"
	"github.com/sei-protocol/sei-chain/app/retiredoracle"
	"github.com/sei-protocol/sei-chain/sei-cosmos/std"
)

// MakeEncodingConfig creates an EncodingConfig for testing.
func MakeEncodingConfig() params.EncodingConfig {
	encodingConfig := params.MakeEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredibcgov.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredoracle.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredoracle.RegisterLegacyAminoCodec(encodingConfig.Amino)
	return encodingConfig
}

// MakeLegacyEncodingConfig creates an EncodingConfig for testing.
func MakeLegacyEncodingConfig() params.EncodingConfig {
	encodingConfig := params.MakeLegacyEncodingConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredibcgov.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredoracle.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	retiredoracle.RegisterLegacyAminoCodec(encodingConfig.Amino)
	return encodingConfig
}
