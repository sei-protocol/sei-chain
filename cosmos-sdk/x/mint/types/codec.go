package types

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	cryptocodec "github.com/sei-protocol/sei-chain/cosmos-sdk/crypto/codec"
)

var (
	amino = codec.NewLegacyAmino()
)

func init() {
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}
