package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const TypeMsgEVMTransaction = "evm_transaction"

var _ sdk.Msg = &MsgEVMTransaction{}

func NewMsgEVMTransaction() *MsgEVMTransaction {
	return &MsgEVMTransaction{}
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
