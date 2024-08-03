package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetPositionEffectFromStr(t *testing.T) {
	effect := "Close"
	expected := types.PositionEffect_CLOSE
	actual, err := types.GetPositionEffectFromStr(effect)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified effect
	effect = "invalid_effect"
	_, err = types.GetPositionEffectFromStr(effect)

	require.NotNil(t, err)
}

func TestGetPositionDirectionFromStr(t *testing.T) {
	direction := "Long"
	expected := types.PositionDirection_LONG
	actual, err := types.GetPositionDirectionFromStr(direction)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified direction
	direction = "invalid_direction"
	_, err = types.GetPositionEffectFromStr(direction)

	require.NotNil(t, err)
}

func TestGetOrderTypeFromStr(t *testing.T) {
	orderType := "Market"
	expected := types.OrderType_MARKET
	actual, err := types.GetOrderTypeFromStr(orderType)

	require.Nil(t, err)
	require.Equal(t, expected, actual)

	// unidentified direction
	orderType = "invalid"
	_, err = types.GetPositionEffectFromStr(orderType)

	require.NotNil(t, err)
}
