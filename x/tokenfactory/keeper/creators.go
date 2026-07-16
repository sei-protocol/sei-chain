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

// GetAllDenomsFromCreator returns every denom for a creator with no page cap.
// Safe to use in the wasm query path: gas metering bounds execution cost, so unbounded iteration does not pose a DoS risk.
func (k Keeper) GetAllDenomsFromCreator(ctx sdk.Context, creator string) []string {
	store := k.GetCreatorPrefixStore(ctx, creator)
	iterator := store.Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()
	var denoms []string
	for ; iterator.Valid(); iterator.Next() {
		denoms = append(denoms, string(iterator.Key()))
	}
	return denoms
}

func (k Keeper) GetAllDenomsIterator(ctx sdk.Context) sdk.Iterator {
	return k.GetCreatorsPrefixStore(ctx).Iterator(nil, nil)
}
