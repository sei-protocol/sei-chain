package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type ContractDepositInfo struct {
	Account string  `json:"account"`
	Denom   string  `json:"denom"`
	Amount  sdk.Dec `json:"amount"`
}

func (d *DepositInfoEntry) ToContractDepositInfo() ContractDepositInfo {
	return ContractDepositInfo{
		Account: d.Creator,
		Denom:   d.Denom,
		Amount:  d.Amount,
	}
}
