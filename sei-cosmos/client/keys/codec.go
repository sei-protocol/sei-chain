package keys

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
)

// TODO: remove this file https://github.com/cosmos/cosmos-sdk/issues/8047

// KeysCdc defines codec to be used with key operations
var KeysCdc *codec.LegacyAmino

func init() {
	KeysCdc = codec.NewLegacyAmino()
	cryptocodec.RegisterCrypto(KeysCdc)
	KeysCdc.Seal()
}

// marshal keys
func MarshalJSON(o interface{}) ([]byte, error) {
	return KeysCdc.MarshalAsJSON(o)
}

// unmarshal json
func UnmarshalJSON(bz []byte, ptr interface{}) error {
	return KeysCdc.UnmarshalAsJSON(bz, ptr)
}
