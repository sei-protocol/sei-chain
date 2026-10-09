package keeper

import (
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// MigrateDelegationByValIndexResult reports the outcome of populating the
// delegation-by-validator index.
type MigrateDelegationByValIndexResult struct {
	TotalDelegations int
	AlreadyReady     bool
	Elapsed          time.Duration
}

// DelegationByValIndexReady reports whether the delegation-by-validator index is
// populated in the state this context reads.
func (k Keeper) DelegationByValIndexReady(ctx sdk.Context) bool {
	return ctx.KVStore(k.storeKey).Has(types.DelegationByValIndexReadyKey)
}

// MigrateDelegationByValIndex writes an index entry for every stored delegation and
// marks the index ready. It is a no-op once the index is ready.
func (k Keeper) MigrateDelegationByValIndex(ctx sdk.Context) (MigrateDelegationByValIndexResult, error) {
	start := time.Now()
	store := ctx.KVStore(k.storeKey)

	if k.DelegationByValIndexReady(ctx) {
		return MigrateDelegationByValIndexResult{AlreadyReady: true, Elapsed: time.Since(start)}, nil
	}

	result := MigrateDelegationByValIndexResult{}
	iterator := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	defer func() { _ = iterator.Close() }()

	// SetDelegation writes no index entry while the index is not ready, so none
	// exists yet and each one can be written without checking for it first.
	for ; iterator.Valid(); iterator.Next() {
		delegation, err := types.UnmarshalDelegation(k.cdc, iterator.Value())
		if err != nil {
			return result, fmt.Errorf("unmarshal delegation at key %X: %w", iterator.Key(), err)
		}
		delAddr, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		if err != nil {
			return result, fmt.Errorf("parse delegator address %q: %w", delegation.DelegatorAddress, err)
		}
		store.Set(types.GetDelegationByValIndexKey(delAddr, delegation.GetValidatorAddr()), []byte{})
		result.TotalDelegations++
	}

	store.Set(types.DelegationByValIndexReadyKey, []byte{})
	result.Elapsed = time.Since(start)
	return result, nil
}
