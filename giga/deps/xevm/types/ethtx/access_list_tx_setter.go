package ethtx

import sdk "github.com/cosmos/cosmos-sdk/types"

func (tx *AccessListTx) SetTo(v string) {
	tx.To = v
}

func (tx *AccessListTx) SetAmount(v sdk.Int) {
	tx.Amount = &v
}

func (tx *AccessListTx) SetGasPrice(v sdk.Int) {
	tx.GasPrice = &v
}

func (tx *AccessListTx) SetAccesses(v AccessList) {
	tx.Accesses = v
}
