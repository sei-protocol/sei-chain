package wasm

import (
	"strings"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

//nolint:staticcheck // following the linter here requires changes in the sdk, I reckon.
func GetContractPositionDirection(direction types.PositionDirection) string {
	return strings.Title(strings.ToLower(types.PositionDirection_name[int32(direction)]))
}

//nolint:staticcheck // following the linter here requires changes in the sdk, I reckon.
func GetContractOrderType(orderType types.OrderType) string {
	return strings.Title(strings.ToLower(types.OrderType_name[int32(orderType)]))
}
