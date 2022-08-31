package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func (k Keeper) AddCreatorToWhitelist(ctx sdk.Context, creator string) {
	store := ctx.KVStore(k.storeKey)
	// Value here does not matter, kv used as simple set - just checking inclusion
	store.Set(types.GetCreateDenomFeeWhitelistPrefix(creator), []byte(creator))
}

// Checks whether creator address is in create denom fee whitelist
func (k Keeper) CreatorInDenomWhitelist(ctx sdk.Context, creator string) (found bool) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetCreateDenomFeeWhitelistPrefix(creator))
	if b == nil {
		return false
	}
	return true
}
