package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAssetType(t *testing.T) {
	asset := types.Asset{AssetType: types.AssetType_TYPECW20}
	require.True(t, asset.IsCW20())
	asset.AssetType = types.AssetType_TYPECW721
	require.True(t, asset.IsCW721())
	asset.AssetType = types.AssetType_TYPENATIVE
	require.True(t, asset.IsNative())
}
