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
	contractDependencies := []*types.ContractDependencyInfo{
		{
			Dependency: "dependency1",
		},
		{
			Dependency: "dependency2",
		},
	}

	contractInfo := types.ContractInfoV2{
		CodeId:       uint64(1),
		ContractAddr: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Dependencies: contractDependencies,
	}

	pairList := []types.Pair{
		{
			PriceDenom:       "USDC",
			AssetDenom:       "SEI",
			PriceTicksize:    &keepertest.TestTicksize,
			QuantityTicksize: &keepertest.TestTicksize,
		},
	}

	priceList := []types.ContractPairPrices{
		{
			PricePair: pairList[0],
			Prices: []*types.Price{
				{
					SnapshotTimestampInSeconds: 2,
					Pair:                       &(pairList[0]),
					Price:                      sdk.MustNewDecFromStr("101"),
				},
			},
		},
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
		PairList:     pairList,
		PriceList:    priceList,
		NextOrderId:  10,
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
	require.ElementsMatch(t, genesisState.ContractState[0].PairList, got.ContractState[0].PairList)
	require.Equal(t, genesisState.ContractState[0].ContractInfo.CodeId, got.ContractState[0].ContractInfo.CodeId)
	require.Equal(t, genesisState.ContractState[0].ContractInfo.ContractAddr, got.ContractState[0].ContractInfo.ContractAddr)
	require.ElementsMatch(t, genesisState.ContractState[0].ContractInfo.Dependencies, got.ContractState[0].ContractInfo.Dependencies)
	require.ElementsMatch(t, genesisState.ContractState[0].PriceList, got.ContractState[0].PriceList)
	require.Equal(t, genesisState.ContractState[0].NextOrderId, got.ContractState[0].NextOrderId)
	// this line is used by starport scaffolding # genesis/test/assert
}
