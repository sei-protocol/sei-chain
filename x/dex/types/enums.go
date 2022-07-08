package types

import (
	"errors"
	"fmt"
	"strings"
)

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

func GetContractPositionDirection(direction PositionDirection) string {
	return strings.Title(strings.ToLower(PositionDirection_name[int32(direction)]))
}

func GetContractPositionEffect(effect PositionEffect) string {
	return strings.Title(strings.ToLower(PositionEffect_name[int32(effect)]))
}

func GetContractOrderType(orderType OrderType) string {
	return strings.Title(strings.ToLower(OrderType_name[int32(orderType)]))
}
