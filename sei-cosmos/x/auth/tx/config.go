package tx

import (
	"fmt"

	signingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
)

type config struct {
	handler     signing.SignModeHandler
	decoder     sdk.TxDecoder
	encoder     sdk.TxEncoder
	jsonDecoder sdk.TxDecoder
	jsonEncoder sdk.TxEncoder
	protoCodec  codec.ProtoCodecMarshaler
}

// NewTxConfig returns a new protobuf TxConfig using the provided ProtoCodec and sign modes. The
// first enabled sign mode will become the default sign mode.
// NOTE: Use NewTxConfigWithHandler to provide a custom signing handler in case the sign mode
// is not supported by default (eg: SignMode_SIGN_MODE_EIP_191).
func NewTxConfig(protoCodec codec.ProtoCodecMarshaler, enabledSignModes []signingtypes.SignMode) client.TxConfig {
	return NewTxConfigWithHandler(protoCodec, makeSignModeHandler(enabledSignModes))
}

// NewTxConfig returns a new protobuf TxConfig using the provided ProtoCodec and signing handler.
func NewTxConfigWithHandler(protoCodec codec.ProtoCodecMarshaler, handler signing.SignModeHandler) client.TxConfig {
	return &config{
		handler:     handler,
		decoder:     DefaultTxDecoder(protoCodec),
		encoder:     DefaultTxEncoder(),
		jsonDecoder: DefaultJSONTxDecoder(protoCodec),
		jsonEncoder: DefaultJSONTxEncoder(protoCodec),
		protoCodec:  protoCodec,
	}
}

// NewTxConfigWithoutBodyBloatRejection returns a TxConfig whose decoder does not
// reject non-canonical (body-bloat) protobuf tx bodies, so historical blocks whose
// tx bodies predate that check can be decoded and executed. This is consensus-unsafe
// for live paths and exists only for historical replay tooling gated behind a build tag.
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

func (g config) NewTxBuilder() client.TxBuilder {
	return newBuilder()
}

// WrapTxBuilder returns a builder from provided transaction
func (g config) WrapTxBuilder(newTx sdk.Tx) (client.TxBuilder, error) {
	newBuilder, ok := newTx.(*wrapper)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", &wrapper{}, newTx)
	}

	return newBuilder, nil
}

func (g config) SignModeHandler() signing.SignModeHandler {
	return g.handler
}

func (g config) TxEncoder() sdk.TxEncoder {
	return g.encoder
}

func (g config) TxDecoder() sdk.TxDecoder {
	return g.decoder
}

func (g config) ProtoCodec() codec.ProtoCodecMarshaler {
	return g.protoCodec
}

func (g config) TxJSONEncoder() sdk.TxEncoder {
	return g.jsonEncoder
}

func (g config) TxJSONDecoder() sdk.TxDecoder {
	return g.jsonDecoder
}
