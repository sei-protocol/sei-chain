package utils_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TestGetPositionEffectFromStr(t *testing.T) {
	effect := "Close"
	expected := types.PositionEffect_CLOSE
	actual, err := utils.GetPositionEffectFromStr(effect)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified effect
	effect = "invalid_effect"
	_, err = utils.GetPositionEffectFromStr(effect)

	require.NotNil(t, err)
}

func TestGetPositionDirectionFromStr(t *testing.T) {
	direction := "Long"
	expected := types.PositionDirection_LONG
	actual, err := utils.GetPositionDirectionFromStr(direction)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified direction
	direction = "invalid_direction"
	_, err = utils.GetPositionEffectFromStr(direction)

	require.NotNil(t, err)
}

func TestGetOrderTypeFromStr(t *testing.T) {
	orderType := "Market"
	expected := types.OrderType_MARKET
	actual, err := utils.GetOrderTypeFromStr(orderType)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified direction
	orderType = "invalid"
	_, err = utils.GetPositionEffectFromStr(orderType)

	require.NotNil(t, err)
}
