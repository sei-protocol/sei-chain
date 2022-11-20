package dex_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	}
}

func TestGenesis(t *testing.T) {
	contractList := []types.ContractState{}
	contractInfo := types.ContractInfoV2{
		CodeId:       uint64(1),
		ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
	}
	contractState := types.ContractState{
		LongBookList: []types.LongBook{
			{
				Price: sdk.NewDec(1),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
				},
			},
		},
		ShortBookList: []types.ShortBook{
			{
				Price: sdk.NewDec(1),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
				},
			},
		},
		ContractInfo: contractInfo,
	}
	contractList = append(contractList, contractState)

	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		// this line is used by starport scaffolding # genesis/test/state
		ContractState: contractList,
	}

	k, ctx := keepertest.DexKeeper(t)
	k.SetContract(ctx, &contractInfo)
	dex.InitGenesis(ctx, *k, genesisState)
	got := dex.ExportGenesis(ctx, *k)
	require.NotNil(t, got)

	require.ElementsMatch(t, genesisState.ContractState[0].LongBookList, got.ContractState[0].LongBookList)
	require.ElementsMatch(t, genesisState.ContractState[0].ShortBookList, got.ContractState[0].ShortBookList)
	// this line is used by starport scaffolding # genesis/test/assert
}
