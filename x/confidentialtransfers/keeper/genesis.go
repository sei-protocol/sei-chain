package keeper

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (k BaseKeeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)
	k.SetParams(ctx, gs.Params)
	for i := range gs.Accounts {
		genesisCtAccount := gs.Accounts[i]
		store := ctx.KVStore(k.storeKey)
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
	supplyStore := prefix.NewStore(store, types.AccountsKey)

	genesisAccounts := make([]types.GenesisCtAccount, 0)
	pageRes, err := query.Paginate(supplyStore, pagination, func(key, value []byte) error {
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
