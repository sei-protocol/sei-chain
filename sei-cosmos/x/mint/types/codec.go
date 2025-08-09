package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
)

var (
	amino = codec.NewLegacyAmino()
)

func init() {
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}
