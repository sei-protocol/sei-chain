package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex"
	"github.com/stretchr/testify/assert"
)

func TestIsDecimalMultipleOf(t *testing.T) {
	v1, _ := sdk.NewDecFromStr("2.4")
	v2, _ := sdk.NewDecFromStr("1.2")
	v3, _ := sdk.NewDecFromStr("2")
	v4, _ := sdk.NewDecFromStr("100.5")
	v5, _ := sdk.NewDecFromStr("0.5")
	v6, _ := sdk.NewDecFromStr("1.5")
	v7, _ := sdk.NewDecFromStr("1.01")
	v8, _ := sdk.NewDecFromStr("3")
	v9, _ := sdk.NewDecFromStr("5.4")
	v10, _ := sdk.NewDecFromStr("0.3")

	assert.True(t, dex.IsDecimalMultipleOf(v1, v2))
	assert.True(t, !dex.IsDecimalMultipleOf(v2, v1))
	assert.True(t, !dex.IsDecimalMultipleOf(v3, v2))
	assert.True(t, dex.IsDecimalMultipleOf(v3, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v3, v6))
	assert.True(t, dex.IsDecimalMultipleOf(v4, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v2, v1))
	assert.True(t, dex.IsDecimalMultipleOf(v6, v5))
	assert.True(t, !dex.IsDecimalMultipleOf(v7, v3))
	assert.True(t, dex.IsDecimalMultipleOf(v8, v6))
	assert.True(t, dex.IsDecimalMultipleOf(v9, v10))
}
