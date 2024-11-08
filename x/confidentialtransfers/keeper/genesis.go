package keeper

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

func (k BaseKeeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) {
	k.SetParams(ctx, gs.Params)
	store := ctx.KVStore(k.storeKey)
	for i := range gs.Accounts {
		genesisCtAccount := gs.Accounts[i]

		bz := k.cdc.MustMarshal(&genesisCtAccount.Account) // Marshal the Account object into bytes
		store.Set(genesisCtAccount.Key, bz)
	}
}

func (k BaseKeeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	genesisCtAccounts, _, err := k.GetPaginatedAccounts(ctx, &query.PageRequest{Limit: query.MaxLimit})
	if err != nil {
		panic(fmt.Errorf("failed to fetch genesis ct accounts: %w", err))
	}
	return types.NewGenesisState(
		k.GetParams(ctx),
		genesisCtAccounts,
	)
}

func (k BaseKeeper) GetPaginatedAccounts(ctx sdk.Context, pagination *query.PageRequest) ([]types.GenesisCtAccount, *query.PageResponse, error) {
	store := ctx.KVStore(k.storeKey)

	genesisAccounts := make([]types.GenesisCtAccount, 0)
	pageRes, err := query.Paginate(store, pagination, func(key, value []byte) error {
		var ctAccount types.CtAccount
		err := ctAccount.Unmarshal(value)
		if err != nil {
			return err
		}

		genesisAccounts = append(genesisAccounts, types.GenesisCtAccount{Key: key, Account: ctAccount})
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return genesisAccounts, pageRes, nil
}
