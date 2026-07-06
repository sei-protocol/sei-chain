//go:build !test_amino && historical_replay

package params

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
)

// This variant swaps the block-execution tx decoder to the lenient one that does
// not reject non-canonical protobuf tx bodies, so a tagged build can replay
// historical blocks whose tx bodies predate strict body-bloat rejection. It is
// consensus-unsafe for live paths and must only be reachable via the
// historical_replay build tag.

// MakeEncodingConfig creates an EncodingConfig whose TxConfig uses the lenient
// (no body-bloat rejection) decoder, for historical-replay builds only.
func MakeEncodingConfig() EncodingConfig {
	amino := codec.NewLegacyAmino()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	txCfg := tx.NewTxConfigWithoutBodyBloatRejection(marshaler, tx.DefaultSignModes)

	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          txCfg,
		Amino:             amino,
	}
}

// MakeLegacyEncodingConfig creates a legacy EncodingConfig whose TxConfig uses the
// lenient (no body-bloat rejection) decoder, for historical-replay builds only.
func MakeLegacyEncodingConfig() EncodingConfig {
	amino := codec.NewLegacyAmino()
	interfaceRegistry := types.NewLegacyInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	txCfg := tx.NewTxConfigWithoutBodyBloatRejection(marshaler, tx.DefaultSignModes)

	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          txCfg,
		Amino:             amino,
	}
}
