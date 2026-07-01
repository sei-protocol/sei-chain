//go:build historical_replay

package tx

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	signingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

// NewTxConfigWithoutBodyBloatRejection returns a TxConfig whose decoder does not
// reject non-canonical (body-bloat) protobuf tx bodies, so historical blocks whose
// tx bodies predate that check can be decoded and executed.
//
// It is consensus-unsafe for live paths, so it is compiled ONLY into
// historical_replay-tagged builds — an untagged (production) binary cannot reach
// it, keeping the lenient execution decoder off every mempool/CheckTx/DeliverTx path.
func NewTxConfigWithoutBodyBloatRejection(protoCodec codec.ProtoCodecMarshaler, enabledSignModes []signingtypes.SignMode) client.TxConfig {
	return &config{
		handler:     makeSignModeHandler(enabledSignModes),
		decoder:     DefaultTxDecoderWithoutBodyBloatRejection(protoCodec),
		encoder:     DefaultTxEncoder(),
		jsonDecoder: DefaultJSONTxDecoder(protoCodec),
		jsonEncoder: DefaultJSONTxEncoder(protoCodec),
		protoCodec:  protoCodec,
	}
}
