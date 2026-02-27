package feegrant_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
)

func TestMsgGrantAllowance(t *testing.T) {
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	addr, _ := sdk.AccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl")
	addr2, _ := sdk.AccAddressFromBech32("sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz")
	atom := sdk.NewCoins(sdk.NewInt64Coin("atom", 555))
	threeHours := time.Now().Add(3 * time.Hour)
	basic := &feegrant.BasicAllowance{
		SpendLimit: atom,
		Expiration: &threeHours,
	}

	cases := map[string]struct {
		grantee sdk.AccAddress
		granter sdk.AccAddress
		grant   *feegrant.BasicAllowance
		valid   bool
	}{
		"valid": {
			grantee: addr,
			granter: addr2,
			grant:   basic,
			valid:   true,
		},
		"no grantee": {
			granter: addr2,
			grantee: sdk.AccAddress{},
			grant:   basic,
			valid:   false,
		},
		"no granter": {
			granter: sdk.AccAddress{},
			grantee: addr,
			grant:   basic,
			valid:   false,
		},
		"grantee == granter": {
			grantee: addr,
			granter: addr,
			grant:   basic,
			valid:   false,
		},
	}

	for _, tc := range cases {
		msg, err := feegrant.NewMsgGrantAllowance(tc.grant, tc.granter, tc.grantee)
		require.NoError(t, err)
		err = msg.ValidateBasic()

		if tc.valid {
			require.NoError(t, err)

			addrSlice := msg.GetSigners()
			require.Equal(t, tc.granter.String(), addrSlice[0].String())

			allowance, err := msg.GetFeeAllowanceI()
			require.NoError(t, err)
			require.Equal(t, tc.grant, allowance)

			err = msg.UnpackInterfaces(cdc)
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestMsgRevokeAllowance(t *testing.T) {
	addr, _ := sdk.AccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl")
	addr2, _ := sdk.AccAddressFromBech32("sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz")
	atom := sdk.NewCoins(sdk.NewInt64Coin("atom", 555))
	threeHours := time.Now().Add(3 * time.Hour)

	basic := &feegrant.BasicAllowance{
		SpendLimit: atom,
		Expiration: &threeHours,
	}
	cases := map[string]struct {
		grantee sdk.AccAddress
		granter sdk.AccAddress
		grant   *feegrant.BasicAllowance
		valid   bool
	}{
		"valid": {
			grantee: addr,
			granter: addr2,
			grant:   basic,
			valid:   true,
		},
		"no grantee": {
			granter: addr2,
			grantee: sdk.AccAddress{},
			grant:   basic,
			valid:   false,
		},
		"no granter": {
			granter: sdk.AccAddress{},
			grantee: addr,
			grant:   basic,
			valid:   false,
		},
		"grantee == granter": {
			grantee: addr,
			granter: addr,
			grant:   basic,
			valid:   false,
		},
	}

	for _, tc := range cases {
		msg := feegrant.NewMsgRevokeAllowance(tc.granter, tc.grantee)
		err := msg.ValidateBasic()
		if tc.valid {
			require.NoError(t, err)
			addrSlice := msg.GetSigners()
			require.Equal(t, tc.granter.String(), addrSlice[0].String())
		} else {
			require.Error(t, err)
		}
	}
}
