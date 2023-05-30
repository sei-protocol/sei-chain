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
