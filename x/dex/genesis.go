package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// InitGenesis initializes the dex module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	k.CreateModuleAccount(ctx)

	// Set all the longBook
	for _, contractState := range genState.ContractState {

		contractInfo := contractState.ContractInfo
		err := k.SetContract(ctx, &contractInfo)
		if err != nil {
			panic(err)
		}

		for _, elem := range contractState.PairList {
			k.AddRegisteredPair(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.LongBookList {
			k.SetLongBook(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.ShortBookList {
			k.SetShortBook(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.PriceList {
			for _, priceElem := range elem.Prices {
				k.SetPriceState(ctx, *priceElem, contractState.ContractInfo.ContractAddr)
			}
		}

		k.SetNextOrderID(ctx, contractState.ContractInfo.ContractAddr, contractState.NextOrderId)

	}

	// this line is used by starport scaffolding # genesis/module/init
	k.SetParams(ctx, genState.Params)

	k.SetEpoch(ctx, genState.LastEpoch)
}

// ExportGenesis returns the dex module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	allContractInfo := k.GetAllContractInfo(ctx)
	contractStates := make([]types.ContractState, len(allContractInfo))
	for i, contractInfo := range allContractInfo {
		contractAddr := contractInfo.ContractAddr
		registeredPairs := k.GetAllRegisteredPairs(ctx, contractAddr)
		// Save all price info for contract, for all its pairs
		contractPrices := []types.ContractPairPrices{}
		for _, elem := range registeredPairs {
			pairPrices := k.GetAllPrices(ctx, contractAddr, elem)
			contractPrices = append(contractPrices, types.ContractPairPrices{
				PricePair: elem,
				Prices:    pairPrices,
			})
		}
		contractStates[i] = types.ContractState{
			ContractInfo:  contractInfo,
			LongBookList:  k.GetAllLongBook(ctx, contractAddr),
			ShortBookList: k.GetAllShortBook(ctx, contractAddr),
			PairList:      registeredPairs,
			PriceList:     contractPrices,
			NextOrderId:   k.GetNextOrderID(ctx, contractAddr),
		}
	}
	genesis.ContractState = contractStates

	_, currentEpoch := k.IsNewEpoch(ctx)
	genesis.LastEpoch = currentEpoch

	return genesis
}
