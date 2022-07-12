package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	LongPositionDirection  string = "Long"
	ShortPositionDirection string = "Short"
	OpenPositionEffect     string = "Open"
	ClosePositionEffect    string = "Close"
)

type SudoSettlementMsg struct {
	Settlement Settlements `json:"settlement"`
}

func NewSettlementEntry(
	orderID uint64,
	account string,
	direction PositionDirection,
	priceDenom string,
	assetDenom string,
	quantity sdk.Dec,
	executionCostOrProceed sdk.Dec,
	expectedCostOrProceed sdk.Dec,
	orderType OrderType,
) *SettlementEntry {
	return &SettlementEntry{
		OrderId:                orderID,
		PositionDirection:      GetContractPositionDirection(direction),
		PriceDenom:             priceDenom,
		AssetDenom:             assetDenom,
		Quantity:               quantity,
		ExecutionCostOrProceed: executionCostOrProceed,
		ExpectedCostOrProceed:  expectedCostOrProceed,
		Account:                account,
		OrderType:              GetContractOrderType(orderType),
	}
}
