//go:build test_amino

package params

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

// MakeEncodingConfig creates an EncodingConfig for an amino based test configuration.
func MakeEncodingConfig() EncodingConfig {
	cdc := codec.New()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewAminoCodec(cdc)

	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          authtypes.StdTxConfig{Cdc: cdc},
		Amino:             cdc,
	}
}

// MakeLegacyEncodingConfig creates an EncodingConfig for an amino based test configuration.
func MakeLegacyEncodingConfig() EncodingConfig {
	cdc := codec.New()
	interfaceRegistry := types.NewLegacyInterfaceRegistry()
	marshaler := codec.NewAminoCodec(cdc)

	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          authtypes.StdTxConfig{Cdc: cdc},
		Amino:             cdc,
	}
}
