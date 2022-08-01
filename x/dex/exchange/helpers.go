package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type ExecutionOutcome struct {
	TotalNotional sdk.Dec
	TotalQuantity sdk.Dec
	Settlements   []*types.SettlementEntry
}

func (o *ExecutionOutcome) Merge(other *ExecutionOutcome) ExecutionOutcome {
	return ExecutionOutcome{
		TotalNotional: o.TotalNotional.Add(other.TotalNotional),
		TotalQuantity: o.TotalQuantity.Add(other.TotalQuantity),
		Settlements:   append(o.Settlements, other.Settlements...),
	}
}
