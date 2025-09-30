package keeper

import (
	"github.com/sei-protocol/sei-chain/x/seinet/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetCovenant returns the covenant for the provided id if it exists.
func (k Keeper) GetCovenant(ctx sdk.Context, covenantID string) (types.Covenant, bool) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.CovenantKey(covenantID))
	if bz == nil {
		return types.Covenant{}, false
	}

	var covenant types.Covenant
	k.cdc.MustUnmarshal(bz, &covenant)
	return covenant, true
}

// SetCovenant stores the provided covenant state.
func (k Keeper) SetCovenant(ctx sdk.Context, covenant types.Covenant) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&covenant)
	store.Set(types.CovenantKey(covenant.Id), bz)
}

// RemoveCovenant deletes the covenant with the given id from the store.
func (k Keeper) RemoveCovenant(ctx sdk.Context, covenantID string) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.CovenantKey(covenantID))
}
