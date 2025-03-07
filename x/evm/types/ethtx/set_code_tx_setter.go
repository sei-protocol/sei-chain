package ethtx

import sdk "github.com/cosmos/cosmos-sdk/types"

func (tx *SetCodeTx) SetTo(v string) {
	tx.To = v
}

func (tx *SetCodeTx) SetAmount(v sdk.Int) {
	tx.Amount = &v
}

func (tx *SetCodeTx) SetGasFeeCap(v sdk.Int) {
	tx.GasFeeCap = &v
}

func (tx *SetCodeTx) SetGasTipCap(v sdk.Int) {
	tx.GasTipCap = &v
}

func (tx *SetCodeTx) SetAccesses(v AccessList) {
	tx.Accesses = v
}

func (tx *SetCodeTx) SetAuthList(v AuthList) {
	tx.AuthList = v
}
