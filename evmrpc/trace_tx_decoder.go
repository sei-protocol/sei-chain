package evmrpc

import (
	"golang.org/x/mod/semver"

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

func traceCompatTxConfig(txConfig client.TxConfig, ctx sdk.Context) client.TxConfig {
	if semver.Compare(ctx.ClosestUpgradeName(), "v6.5") >= 0 {
		return txConfig
	}
	provider, ok := txConfig.(protoCodecProvider)
	if !ok {
		return txConfig
	}
	return traceTxConfig{
		TxConfig: txConfig,
		decoder:  authtx.DefaultTxDecoderWithoutBodyBloatRejection(provider.ProtoCodec()),
	}
}

func traceCompatTxConfigProvider(ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig) func(int64) client.TxConfig {
	return func(height int64) client.TxConfig {
		return traceCompatTxConfig(txConfigProvider(height), ctxProvider(height))
	}
}

func traceCompatTxDecoder(txConfig client.TxConfig, ctx sdk.Context) sdk.TxDecoder {
	return traceCompatTxConfig(txConfig, ctx).TxDecoder()
}
