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
			denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/bitcoin",
			valid: true,
		},
		{
			desc:  "multiple slashes in subdenom",
			denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/bitcoin/1",
			valid: true,
		},
		{
			desc:  "no subdenom",
			denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/",
			valid: true,
		},
		{
			desc:  "incorrect prefix",
			denom: "ibc/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/bitcoin",
			valid: false,
		},
		{
			desc:  "subdenom of only slashes",
			denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/////",
			valid: true,
		},
		{
			desc:  "too long name",
			denom: "factory/sei1t7egva48prqmzl59x5ngv4zx0dtrwewc9m7z44/adsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsfadsf",
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
