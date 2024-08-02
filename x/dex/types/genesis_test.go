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
				Params: types.DefaultParams(),
				ContractState: []types.ContractState{
					{
						LongBookList: []types.LongBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(1),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ShortBookList: []types.ShortBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(1),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ContractInfo: types.ContractInfoV2{
							CodeId:       uint64(1),
							ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
						},
					},
				},
				// this line is used by starport scaffolding # types/genesis/validField
			},
			valid: true,
		},
		{
			desc: "same price multiple markets",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				ContractState: []types.ContractState{
					{
						LongBookList: []types.LongBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "USDC",
									AssetDenom: "ATOM",
								},
							},
						},
						ShortBookList: []types.ShortBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(1),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ContractInfo: types.ContractInfoV2{
							CodeId:       uint64(1),
							ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
						},
					},
				},
				// this line is used by starport scaffolding # types/genesis/validField
			},
			valid: true,
		},
		{
			desc: "duplicated longBook",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				ContractState: []types.ContractState{
					{
						LongBookList: []types.LongBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ContractInfo: types.ContractInfoV2{
							CodeId:       uint64(1),
							ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
						},
					},
				},
			},
			valid: false,
		},
		{
			desc: "duplicated shortBook",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				ContractState: []types.ContractState{
					{
						ShortBookList: []types.ShortBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ContractInfo: types.ContractInfoV2{
							CodeId:       uint64(1),
							ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
						},
					},
				},
			},
			valid: false,
		},
		{
			desc: "invalid contract addr",
			genState: &types.GenesisState{
				Params: types.DefaultParams(),
				ContractState: []types.ContractState{
					{
						ShortBookList: []types.ShortBook{
							{
								Price: sdk.NewDec(0),
								Entry: &types.OrderEntry{
									Price:      sdk.NewDec(0),
									PriceDenom: "SEI",
									AssetDenom: "ATOM",
								},
							},
						},
						ContractInfo: types.ContractInfoV2{
							CodeId:       uint64(1),
							ContractAddr: "invalid",
						},
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
