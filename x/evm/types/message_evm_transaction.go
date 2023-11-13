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
	_ codectypes.UnpackInterfacesMessage = MsgEVMTransaction{}
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
func (msg MsgEVMTransaction) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(msg.Data, new(ethtx.TxData))
}

func (msg MsgEVMTransaction) IsAssociateTx() bool {
	txData, err := UnpackTxData(msg.Data)
	if err != nil {
		// should never happen
		panic(err)
	}
	_, ok := txData.(*ethtx.AssociateTx)
	return ok
}
