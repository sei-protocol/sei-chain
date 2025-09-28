package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
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

func TestMsgClaimSpecificValidateBasic(t *testing.T) {
	msg := types.NewMsgClaimSpecific(
		sdk.AccAddress("acc_________________"),
		common.HexToAddress("0x0123456789abcdef012345abcdef12345678"),
		&types.Asset{AssetType: types.AssetType_TYPECW20, ContractAddress: sdk.AccAddress("contract_______________").String()},
	)
	require.NoError(t, msg.ValidateBasic())

	msg.Claimer = "bad"
	require.Error(t, msg.ValidateBasic())

	msg.Claimer = common.HexToAddress("0x0123456789abcdef012345abcdef12345678").Hex()
	msg.Assets[0].ContractAddress = "bad"
	require.Error(t, msg.ValidateBasic())

	msg.Assets = append(msg.Assets, &types.Asset{AssetType: types.AssetType_TYPENATIVE, Denom: "usei"})
	msg.Assets[0].ContractAddress = sdk.AccAddress("contract_______________").String()
	require.NoError(t, msg.ValidateBasic())

	msg.Assets[1].Denom = ""
	require.Error(t, msg.ValidateBasic())
}
