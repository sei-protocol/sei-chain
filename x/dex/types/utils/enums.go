package utils

import (
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func GetPositionEffectFromStr(str string) (types.PositionEffect, error) {
	val, err := getEnumFromStr(str, types.PositionEffect_value)
	return types.PositionEffect(val), err
}

func GetPositionDirectionFromStr(str string) (types.PositionDirection, error) {
	val, err := getEnumFromStr(str, types.PositionDirection_value)
	return types.PositionDirection(val), err
}

func GetOrderTypeFromStr(str string) (types.OrderType, error) {
	val, err := getEnumFromStr(str, types.OrderType_value)
	return types.OrderType(val), err
}

func getEnumFromStr(str string, enumMap map[string]int32) (int32, error) {
	upperStr := strings.ToUpper(str)
	if val, ok := enumMap[upperStr]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("unknown enum literal: %s", str)
}
