package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
)

func (k Keeper) addDenomFromCreator(ctx sdk.Context, creator, denom string) {
	store := k.GetCreatorPrefixStore(ctx, creator)
	store.Set([]byte(denom), []byte(denom))
}

func (k Keeper) getDenomsFromCreator(ctx sdk.Context, creator string, pagination *query.PageRequest) ([]string, *query.PageResponse, error) {
	store := k.GetCreatorPrefixStore(ctx, creator)
	var denoms []string
	pageRes, err := query.Paginate(store, pagination, func(key []byte, _ []byte) error {
		denoms = append(denoms, string(key))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return denoms, pageRes, nil
}

func (k Keeper) GetAllDenomsIterator(ctx sdk.Context) sdk.Iterator {
	return k.GetCreatorsPrefixStore(ctx).Iterator(nil, nil)
}
