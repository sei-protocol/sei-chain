package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc: "valid genesis state",
			genState: &types.GenesisState{
				LongBookList: []types.LongBook{
					{
						Price: sdk.NewDec(0),
					},
					{
						Price: sdk.NewDec(1),
					},
				},
				ShortBookList: []types.ShortBook{
					{
						Price: sdk.NewDec(0),
					},
					{
						Price: sdk.NewDec(1),
					},
				},
				// this line is used by starport scaffolding # types/genesis/validField
			},
			valid: true,
		},
		{
			desc: "duplicated longBook",
			genState: &types.GenesisState{
				LongBookList: []types.LongBook{
					{
						Price: sdk.NewDec(0),
					},
					{
						Price: sdk.NewDec(0),
					},
				},
			},
			valid: false,
		},
		{
			desc: "duplicated shortBook",
			genState: &types.GenesisState{
				ShortBookList: []types.ShortBook{
					{
						Price: sdk.NewDec(0),
					},
					{
						Price: sdk.NewDec(0),
					},
				},
			},
			valid: false,
		},
		// this line is used by starport scaffolding # types/genesis/testcase
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
