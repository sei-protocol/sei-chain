package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

type DepositInfoEntry struct {
	Creator string
	Denom   string
	Amount  sdk.Dec
}

func (d *DepositInfoEntry) GetAccount() string {
	return d.Creator
}

func (d *DepositInfoEntry) ToContractDepositInfo() wasm.ContractDepositInfo {
	return wasm.ContractDepositInfo{
		Account: d.Creator,
		Denom:   d.Denom,
		Amount:  d.Amount,
	}
}

type DepositInfo struct {
	memStateItems[*DepositInfoEntry]
}

func NewDepositInfo() *DepositInfo {
	return &DepositInfo{memStateItems: NewItems(utils.PtrCopier[DepositInfoEntry])}
}

func (o *DepositInfo) Copy() *DepositInfo {
	return &DepositInfo{memStateItems: *o.memStateItems.Copy()}
}
