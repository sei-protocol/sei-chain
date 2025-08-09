package legacytx

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(StdTx{}, "cosmos-sdk/StdTx", nil)
}
