package types

import (
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
	}
	return 0, fmt.Errorf("unknown enum literal: %s", str)
}
