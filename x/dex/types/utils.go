package types

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func GetContractPositionDirection(direction PositionDirection) string {
	return cases.Title(language.English).String(cases.Lower(language.English).String(PositionDirection_name[int32(direction)]))
}

func GetContractOrderType(orderType OrderType) string {
	return cases.Title(language.English).String(cases.Lower(language.English).String(OrderType_name[int32(orderType)]))
}
