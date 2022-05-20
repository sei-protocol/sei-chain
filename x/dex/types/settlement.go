package types

import (
	"fmt"
	"strconv"
	"strings"
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
	Long                   bool
	PriceSymbol            string
	AssetSymbol            string
	Quantity               uint64
	ExecutionCostOrProceed uint64
	ExpectedCostOrProceed  uint64
	Account                string
	Open                   bool
	Leverage               string
}

func NewSettlement(
	formattedAccount string,
	long bool,
	priceDenom string,
	assetDenom string,
	quantity uint64,
	executionCostOrProceed uint64,
	expectedCostOrProceed uint64,
) *Settlement {
	parts := strings.Split(formattedAccount, FORMATTED_ACCOUNT_DELIMITER)
	return &Settlement{
		Long:                   long,
		PriceSymbol:            priceDenom,
		AssetSymbol:            assetDenom,
		Quantity:               quantity,
		ExecutionCostOrProceed: executionCostOrProceed,
		ExpectedCostOrProceed:  expectedCostOrProceed,
		Account:                parts[0],
		Open:                   parts[1] == OPEN_ORDER_CREATOR_SUFFIX,
		Leverage:               parts[2],
	}
}

func (s *Settlement) String() string {
	return fmt.Sprintf(
		"%s %t %s/%s: %d at %d/%d - %s", s.Account, s.Long, s.PriceSymbol, s.AssetSymbol, s.Quantity, s.ExecutionCostOrProceed, s.ExpectedCostOrProceed, s.Leverage)
}

func (s *Settlement) ToEntry() SettlementEntry {
	return SettlementEntry{
		Account:                s.Account,
		PriceDenom:             s.PriceSymbol,
		AssetDenom:             s.AssetSymbol,
		Quantity:               strconv.FormatUint(s.Quantity, 10),
		ExecutionCostOrProceed: strconv.FormatUint(s.ExecutionCostOrProceed, 10),
		ExpectedCostOrProceed:  strconv.FormatUint(s.ExpectedCostOrProceed, 10),
		PositionDirection:      GetPositionDirection(s.Long),
		PositionEffect:         GetPositionEffect(s.Open),
		Leverage:               s.Leverage,
	}
}

func GetPositionDirection(long bool) string {
	if long {
		return LongPositionDirection
	}
	return ShortPositionDirection
}

func GetPositionEffect(open bool) string {
	if open {
		return OpenPositionEffect
	}
	return ClosePositionEffect
}
