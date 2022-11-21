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
		for _, elem := range contractState.LongBookList {
			k.SetLongBook(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.ShortBookList {
			k.SetShortBook(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.TriggeredOrdersList {
			// not sure if it's guaranteed that the Order has the correct Price/Asset/Contract details...
			k.SetTriggeredOrder(ctx, contractState.ContractInfo.ContractAddr, elem, elem.PriceDenom, elem.AssetDenom)
		}

		for _, elem := range contractState.PairList {
			k.AddRegisteredPair(ctx, contractState.ContractInfo.ContractAddr, elem)
		}

		for _, elem := range contractState.PairList {
			k.SetPriceTickSizeForPair(ctx, contractState.ContractInfo.ContractAddr, elem, *elem.PriceTicksize)
		}

		for _, elem := range contractState.PairList {
			k.SetQuantityTickSizeForPair(ctx, contractState.ContractInfo.ContractAddr, elem, *elem.QuantityTicksize)
		}

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
		contractStates[i] = types.ContractState{
			ContractInfo:        types.ContractInfoV2{},
			LongBookList:        k.GetAllLongBook(ctx, contractAddr),
			ShortBookList:       k.GetAllShortBook(ctx, contractAddr),
			TriggeredOrdersList: k.GetAllTriggeredOrders(ctx, contractAddr),
			PairList:            k.GetAllRegisteredPairs(ctx, contractAddr),
		}
	}
	genesis.ContractState = contractStates

	_, currentEpoch := k.IsNewEpoch(ctx)
	genesis.LastEpoch = currentEpoch

	return genesis
}
