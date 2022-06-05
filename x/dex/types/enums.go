package types

import (
	"errors"
	"fmt"
	"strings"
)

const MICRO_PREFIX = byte('u')

func GetDenomFromStr(str string) (Denom, Unit, error) {
	val, err := getEnumFromStr(str, Denom_value)
	if err != nil {
		if str[0] == MICRO_PREFIX {
			microVal, microErr := getEnumFromStr(str, Denom_value)
			if microErr == nil {
				return Denom(microVal), Unit_MICRO, nil
			}
		}
	}
	return Denom(val), Unit_STANDARD, err
}

func GetPositionEffectFromStr(str string) (PositionEffect, error) {
	val, err := getEnumFromStr(str, PositionEffect_value)
	return PositionEffect(val), err
}

func GetPositionDirectionFromStr(str string) (PositionDirection, error) {
	val, err := getEnumFromStr(str, PositionDirection_value)
	return PositionDirection(val), err
}

func GetOrderTypeFromStr(str string) (OrderType, error) {
	val, err := getEnumFromStr(str, OrderType_value)
	return OrderType(val), err
}

func getEnumFromStr(str string, enumMap map[string]int32) (int32, error) {
	upperStr := strings.ToUpper(str)
	if val, ok := enumMap[upperStr]; ok {
		return val, nil
	} else {
		return 0, errors.New(fmt.Sprintf("Unknown enum literal: %s", str))
	}
}

func GetContractDenomName(denom Denom) string {
	return strings.ToLower(Denom_name[int32(denom)])
}

func GetContractPositionDirection(direction PositionDirection) string {
	return strings.Title(strings.ToLower(PositionDirection_name[int32(direction)]))
}

func GetContractPositionEffect(effect PositionEffect) string {
	return strings.Title(strings.ToLower(PositionEffect_name[int32(effect)]))
}

func GetContractOrderType(orderType OrderType) string {
	return strings.Title(strings.ToLower(OrderType_name[int32(orderType)]))
}
