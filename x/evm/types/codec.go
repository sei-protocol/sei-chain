package types

//nolint:typecheck
import (
	"errors"
	fmt "fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"

	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	AminoCdc  = codec.NewAminoCodec(amino)
)

func init() {
	cryptocodec.RegisterCrypto(amino)
	amino.Seal()
}

func GetAmino() *codec.LegacyAmino {
	return amino
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgEVMTransaction{},
	)
	registry.RegisterInterface(
		"seiprotocol.seichain.evm.TxData",
		(*ethtx.TxData)(nil),
		&ethtx.DynamicFeeTx{},
		&ethtx.AccessListTx{},
		&ethtx.LegacyTx{},
		&ethtx.BlobTx{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

func PackTxData(txData ethtx.TxData) (*codectypes.Any, error) {
	msg, ok := txData.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("cannot proto marshal %T", txData)
	}

	anyTxData, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	return anyTxData, nil
}

func UnpackTxData(any *codectypes.Any) (ethtx.TxData, error) {
	if any == nil {
		return nil, errors.New("protobuf Any message cannot be nil")
	}

	txData, ok := any.GetCachedValue().(ethtx.TxData)
	if !ok {
		return nil, fmt.Errorf("cannot unpack Any into TxData %T", any)
	}

	return txData, nil
}
