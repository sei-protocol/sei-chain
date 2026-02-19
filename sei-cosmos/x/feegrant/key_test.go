package feegrant_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
)

func TestMarshalAndUnmarshalFeegrantKey(t *testing.T) {
	grantee, err := sdk.AccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl")
	require.NoError(t, err)
	granter, err := sdk.AccAddressFromBech32("sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz")
	require.NoError(t, err)

	key := feegrant.FeeAllowanceKey(granter, grantee)
	require.Len(t, key, len(grantee.Bytes())+len(granter.Bytes())+3)
	require.Equal(t, feegrant.FeeAllowancePrefixByGrantee(grantee), key[:len(grantee.Bytes())+2])

	g1, g2 := feegrant.ParseAddressesFromFeeAllowanceKey(key)
	require.Equal(t, granter, g1)
	require.Equal(t, grantee, g2)
}
