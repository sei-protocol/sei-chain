package oracle

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/scripts/common"
	"google.golang.org/grpc"
)

func init() {
	cdc := codec.NewLegacyAmino()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)

	common.TEST_CONFIG = common.EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          tx.NewTxConfig(marshaler, tx.DefaultSignModes),
		Amino:             cdc,
	}
	std.RegisterLegacyAminoCodec(common.TEST_CONFIG.Amino)
	std.RegisterInterfaces(common.TEST_CONFIG.InterfaceRegistry)
	app.ModuleBasics.RegisterLegacyAminoCodec(common.TEST_CONFIG.Amino)
	app.ModuleBasics.RegisterInterfaces(common.TEST_CONFIG.InterfaceRegistry)
}

func run(
	denoms []string,
	prices []uint64,
) {
	grpcConn, _ := grpc.Dial(
		"127.0.0.1:9090",
		grpc.WithInsecure(),
	)
	defer grpcConn.Close()
	common.TX_CLIENT = typestx.NewServiceClient(grpcConn)

	key := common.GetKey(0)
	_ = key
}
