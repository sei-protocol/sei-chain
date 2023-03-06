package wasm

import (
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func GetContractPositionDirection(direction types.PositionDirection) string {
	return cases.Title(language.English).String(cases.Lower(language.English).String(types.PositionDirection_name[int32(direction)]))
}

func GetContractOrderType(orderType types.OrderType) string {
	return cases.Title(language.English).String(cases.Lower(language.English).String(types.OrderType_name[int32(orderType)]))
}
