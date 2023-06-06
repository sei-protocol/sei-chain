package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func TestDecomposeDenoms(t *testing.T) {
	appparams.SetAddressPrefixes()
	for _, tc := range []struct {
		desc  string
		denom string
		valid bool
	}{
		{
			desc:  "empty is invalid",
			denom: "",
			valid: false,
		},
		{
			desc:  "normal",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/bitcoin",
			valid: true,
		},
		{
			desc:  "multiple slashes in subdenom",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/bitcoin/1",
			valid: true,
		},
		{
			desc:  "no subdenom",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/",
			valid: true,
		},
		{
			desc:  "incorrect prefix",
			denom: "ibc/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/bitcoin",
			valid: false,
		},
		{
			desc:  "subdenom of only slashes",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/////",
			valid: true,
		},
		{
			desc:  "too long name",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/adsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsf",
			valid: false,
		},
		{
			desc:  "too long creator name",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjwasdfasdfasdfasdfasdfasdfadfasdfasdfasdfasdfasdfas/bitcoin",
			valid: false,
		},
		{
			desc:  "empty subdenom",
			denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/",
			valid: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, err := types.DeconstructDenom(tc.denom)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetTokenDenom(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		creator  string
		subdenom string
		valid    bool
	}{
		{
			desc:     "normal",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "bitcoin",
			valid:    true,
		},
		{
			desc:     "multiple slashes in subdenom",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "bitcoin/1",
			valid:    true,
		},
		{
			desc:     "no subdenom",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "",
			valid:    true,
		},
		{
			desc:     "subdenom of only slashes",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "/////",
			valid:    true,
		},
		{
			desc:     "too long name",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "adsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsf",
			valid:    false,
		},
		{
			desc:     "subdenom is exactly max length",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "bitcoinfsadfsdfeadfsafwefsefsefsdfsdafasefsf",
			valid:    true,
		},
		{
			desc:     "creator is exactly max length",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjwhjkljkljkljkljkljkljkljkljkljkljk",
			subdenom: "bitcoin",
			valid:    true,
		},
		{
			desc:     "empty subdenom",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "",
			valid:    true,
		},
		{
			desc:     "non standard UTF-8",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "\u2603",
			valid:    false,
		},
		{
			desc:     "non standard ASCII",
			creator:  "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
			subdenom: "\n\t",
			valid:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := types.GetTokenDenom(tc.creator, tc.subdenom)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
