package common

import (
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

type EncodingConfig struct {
	InterfaceRegistry types.InterfaceRegistry
	// NOTE: this field will be renamed to Codec
	Marshaler codec.Codec
	TxConfig  client.TxConfig
	Amino     *codec.LegacyAmino
}

var (
	TEST_CONFIG  EncodingConfig
	TX_CLIENT    typestx.ServiceClient
	TX_HASH_FILE *os.File
	CHAIN_ID     string
)
