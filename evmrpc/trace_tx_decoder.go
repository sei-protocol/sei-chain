package evmrpc

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtx "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
)

type traceTxConfig struct {
	client.TxConfig
	decoder sdk.TxDecoder
}

func (c traceTxConfig) TxDecoder() sdk.TxDecoder {
	return c.decoder
}

type protoCodecProvider interface {
	ProtoCodec() codec.ProtoCodecMarshaler
}

// traceCompatTxConfig selects a historical-compatible decoder for debug_trace*:
//   - pre-v6.5: skip TxBody and AuthInfo canonical-size checks
//   - v6.5 to v6.7: enforce TxBody checks, skip AuthInfo checks
//   - v6.7+: use the live (fully strict) decoder
func traceCompatTxConfig(txConfig client.TxConfig, v65ActiveAtHeight, v67ActiveAtHeight bool) client.TxConfig {
	if v67ActiveAtHeight {
		return txConfig
	}
	provider, ok := txConfig.(protoCodecProvider)
	if !ok {
		return txConfig
	}
	var decoder sdk.TxDecoder
	if v65ActiveAtHeight {
		decoder = authtx.DefaultTxDecoderWithoutAuthInfoBloatRejection(provider.ProtoCodec())
	} else {
		decoder = authtx.DefaultTxDecoderWithoutBodyBloatRejection(provider.ProtoCodec())
	}
	return traceTxConfig{
		TxConfig: txConfig,
		decoder:  decoder,
	}
}

func traceCompatTxConfigProvider(
	txConfigProvider func(int64) client.TxConfig,
	isV65ActiveAtHeight func(int64) bool,
	isV67ActiveAtHeight func(int64) bool,
) func(int64) client.TxConfig {
	return func(height int64) client.TxConfig {
		return traceCompatTxConfig(
			txConfigProvider(height),
			isV65ActiveAtHeight(height),
			isV67ActiveAtHeight(height),
		)
	}
}

func traceCompatTxDecoder(txConfig client.TxConfig, v65ActiveAtHeight, v67ActiveAtHeight bool) sdk.TxDecoder {
	return traceCompatTxConfig(txConfig, v65ActiveAtHeight, v67ActiveAtHeight).TxDecoder()
}
