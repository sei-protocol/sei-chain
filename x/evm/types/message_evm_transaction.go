package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const TypeMsgEVMTransaction = "evm_transaction"

var (
	_ sdk.Msg                            = &MsgEVMTransaction{}
	_ codectypes.UnpackInterfacesMessage = &MsgEVMTransaction{}
	_ sdk.ResultDecorator                = &MsgEVMTransactionResponse{}
)

func NewMsgEVMTransaction(txData proto.Message) (*MsgEVMTransaction, error) {
	txDataAny, err := codectypes.NewAnyWithValue(txData)
	if err != nil {
		return nil, err
	}
	return &MsgEVMTransaction{Data: txDataAny}, nil
}

func (msg *MsgEVMTransaction) Route() string {
	return RouterKey
}

func (msg *MsgEVMTransaction) Type() string {
	return TypeMsgEVMTransaction
}

func (msg *MsgEVMTransaction) GetSigners() []sdk.AccAddress {
	panic("signer should be accessed on EVM transaction level")
}

func (msg *MsgEVMTransaction) GetSignBytes() []byte {
	panic("sign bytes should be accessed on EVM transaction level")
}

func (msg *MsgEVMTransaction) ValidateBasic() error {
	return nil
}

func (msg *MsgEVMTransaction) AsTransaction() (*ethtypes.Transaction, ethtx.TxData) {
	txData, err := UnpackTxData(msg.Data)
	if err != nil {
		return nil, nil
	}

	return ethtypes.NewTx(txData.AsEthereumData()), txData
}

// UnpackInterfaces implements UnpackInterfacesMesssage.UnpackInterfaces
func (msg *MsgEVMTransaction) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(msg.Data, new(ethtx.TxData))
}

func MustGetEVMTransactionMessage(tx sdk.Tx) *MsgEVMTransaction {
	if len(tx.GetMsgs()) != 1 {
		panic("EVM transaction must have exactly 1 message")
	}
	msg, ok := tx.GetMsgs()[0].(*MsgEVMTransaction)
	if !ok {
		panic("not EVM message")
	}
	return msg
}

func (res *MsgEVMTransactionResponse) DecorateSdkResult(sdkRes *sdk.Result) {
	if res == nil {
		return
	}
	if res.VmError != "" {
		sdkRes.EvmError = res.VmError
	}
}
