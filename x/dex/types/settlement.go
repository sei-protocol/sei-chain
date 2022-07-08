package types

import (
	"fmt"
	"strings"

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

type Settlement struct {
	Direction              PositionDirection
	PriceSymbol            string
	AssetSymbol            string
	Quantity               sdk.Dec
	ExecutionCostOrProceed sdk.Dec
	ExpectedCostOrProceed  sdk.Dec
	Account                string
	Effect                 PositionEffect
	Leverage               sdk.Dec
	OrderType              OrderType
}

func NewSettlement(
	formattedAccount string,
	direction PositionDirection,
	priceDenom string,
	assetDenom string,
	quantity sdk.Dec,
	executionCostOrProceed sdk.Dec,
	expectedCostOrProceed sdk.Dec,
	orderType OrderType,
) *Settlement {
	parts := strings.Split(formattedAccount, FORMATTED_ACCOUNT_DELIMITER)
	leverage, _ := sdk.NewDecFromStr(parts[2])
	return &Settlement{
		Direction:              direction,
		PriceSymbol:            priceDenom,
		AssetSymbol:            assetDenom,
		Quantity:               quantity,
		ExecutionCostOrProceed: executionCostOrProceed,
		ExpectedCostOrProceed:  expectedCostOrProceed,
		Account:                parts[0],
		Effect:                 SUFFIX_TO_POSITION_EFFECT[OPEN_ORDER_CREATOR_SUFFIX],
		Leverage:               leverage,
		OrderType:              orderType,
	}
}

func (s *Settlement) String() string {
	return fmt.Sprintf(
		"%s %d %s/%s: %d at %d/%d - %s", s.Account, s.Direction, s.PriceSymbol, s.AssetSymbol, s.Quantity, s.ExecutionCostOrProceed, s.ExpectedCostOrProceed, s.Leverage)
}

func (s *Settlement) ToEntry() SettlementEntry {
	return SettlementEntry{
		Account:                s.Account,
		PriceDenom:             s.PriceSymbol,
		AssetDenom:             s.AssetSymbol,
		Quantity:               s.Quantity,
		ExecutionCostOrProceed: s.ExecutionCostOrProceed,
		ExpectedCostOrProceed:  s.ExpectedCostOrProceed,
		PositionDirection:      GetContractPositionDirection(s.Direction),
		PositionEffect:         GetContractPositionEffect(s.Effect),
		Leverage:               s.Leverage,
		OrderType:              GetContractOrderType(s.OrderType),
	}
}
