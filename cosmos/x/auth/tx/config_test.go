package tx

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/cosmos/std"
	"github.com/sei-protocol/sei-chain/cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/cosmos/types"
	"github.com/sei-protocol/sei-chain/cosmos/x/auth/testutil"
)

func TestGenerator(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	interfaceRegistry.RegisterImplementations((*sdk.Msg)(nil), &testdata.TestMsg{})
	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	suite.Run(t, testutil.NewTxConfigTestSuite(NewTxConfig(protoCodec, DefaultSignModes)))
}
