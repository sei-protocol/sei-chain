package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (k Keeper) AddCreatorToWhitelist(ctx sdk.Context, creator string) {
	store := ctx.KVStore(k.storeKey)
	// Value here does not matter, kv used as simple set - just checking inclusion
	store.Set(types.GetCreatorDenomFeeWhitelistPrefix(creator), []byte(creator))
}

// Checks whether creator address is in create denom fee whitelist
func (k Keeper) IsCreatorInDenomFeeWhitelist(ctx sdk.Context, creator string) (found bool) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetCreatorDenomFeeWhitelistPrefix(creator))
	if b == nil {
		return false
	}
	return true
}

func (k Keeper) GetAllCreatorsDenomFeeWhitelistIterator(ctx sdk.Context) sdk.Iterator {
	return k.GetCreatorsDenomFeePrefixStore(ctx).Iterator(nil, nil)
}

// Checks whether creator address is in create denom fee whitelist
func (k Keeper) GetCreatorsInDenomFeeWhitelist(ctx sdk.Context) []string {
	creators := []string{}

	iterator := k.GetAllCreatorsDenomFeeWhitelistIterator(ctx)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		creator := string(iterator.Value())
		creators = append(creators, creator)
	}

	return creators
}
