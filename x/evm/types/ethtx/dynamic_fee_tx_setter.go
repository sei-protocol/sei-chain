package ethtx

import sdk "github.com/cosmos/cosmos-sdk/types"

func (tx *DynamicFeeTx) SetTo(v string) {
	tx.To = v
}

func (tx *DynamicFeeTx) SetAmount(v sdk.Int) {
	tx.Amount = &v
}

func (tx *DynamicFeeTx) SetGasFeeCap(v sdk.Int) {
	tx.GasFeeCap = &v
}

func (tx *DynamicFeeTx) SetGasTipCap(v sdk.Int) {
	tx.GasTipCap = &v
}

func (tx *DynamicFeeTx) SetAccesses(v AccessList) {
	tx.Accesses = v
}
