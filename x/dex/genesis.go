package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// InitGenesis initializes the capability module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	k.CreateModuleAccount(ctx)

	lastEpoch := uint64(0)

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

		// Set initial tick size for each pair
		// tick size is the minimum unit that can be traded for certain pair
		for _, elem := range contractState.PairList {
			// TODO:(kartik) Is this needed since tick size already part of pair?
			// This might be necessary because tick size store is keyed by pricedenom/assetdenom not full struct
			k.SetDefaultTickSizeForPair(ctx, elem, *elem.Ticksize)
		}

		//TODO:(kartik) Right now looping through all contract states and setting last epoch as latest lastEpoch
		if lastEpoch < contractState.LastEpoch {
			lastEpoch = contractState.LastEpoch
		}

	}

	// this line is used by starport scaffolding # genesis/module/init
	k.SetParams(ctx, genState.Params)

	k.SetEpoch(ctx, lastEpoch)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	allContractInfo := k.GetAllContractInfo(ctx)
	contractStates := make([]types.ContractState, len(allContractInfo))
	for i, contractInfo := range allContractInfo {
		contractAddr := contractInfo.ContractAddr
		matchResult, found := k.GetMatchResultState(ctx, contractAddr)
		if !found {
			matchResult = &types.MatchResult{}

		}
		_, currentEpoch := k.IsNewEpoch(ctx)
		contractStates[i] = types.ContractState{
			ContractInfo:        types.ContractInfoV2{},
			LongBookList:        k.GetAllLongBook(ctx, contractAddr),
			ShortBookList:       k.GetAllShortBook(ctx, contractAddr),
			TriggeredOrdersList: k.GetAllTriggeredOrders(ctx, contractAddr),
			PairList:            k.GetAllRegisteredPairs(ctx, contractAddr),
			MatchResult:         *matchResult,
			// TODO:(kartik) @psu didn't know what to keep for LastEpoch but previously left it at 0
			// Verify currentEpoch should be used
			LastEpoch: currentEpoch,
		}
	}
	genesis.ContractState = contractStates

	return genesis
}
