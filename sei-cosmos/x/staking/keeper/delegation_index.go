package keeper

import (
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// MigrateDelegationByValIndexResult reports the outcome of populating the
// validator-indexed delegation store.
type MigrateDelegationByValIndexResult struct {
	TotalDelegations int
	IndexWritten     int
	AlreadyReady     bool
	Elapsed          time.Duration
}

// DelegationByValIndexReady reports whether the validator-indexed delegation store
// is populated at the version this context reads.
//
// The marker is versioned state written by MigrateDelegationByValIndex, so a context
// reading a height before that migration observes it absent. That makes the answer
// correct for historical queries and re-traced blocks without the caller supplying
// an upgrade name or height.
func (k Keeper) DelegationByValIndexReady(ctx sdk.Context) bool {
	return ctx.KVStore(k.storeKey).Has(types.DelegationByValIndexReadyKey)
}

// MigrateDelegationByValIndex writes a validator-indexed key for every existing
// delegation and then marks the index ready. It is a no-op once the marker is set.
func (k Keeper) MigrateDelegationByValIndex(ctx sdk.Context) (MigrateDelegationByValIndexResult, error) {
	start := time.Now()
	store := ctx.KVStore(k.storeKey)

	if store.Has(types.DelegationByValIndexReadyKey) {
		return MigrateDelegationByValIndexResult{AlreadyReady: true, Elapsed: time.Since(start)}, nil
	}

	result := MigrateDelegationByValIndexResult{}
	iterator := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		delegation, err := types.UnmarshalDelegation(k.cdc, iterator.Value())
		if err != nil {
			return result, fmt.Errorf("unmarshal delegation at key %X: %w", iterator.Key(), err)
		}
		delAddr, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		if err != nil {
			return result, fmt.Errorf("parse delegator address %q: %w", delegation.DelegatorAddress, err)
		}

		result.TotalDelegations++
		indexKey := types.GetDelegationByValIndexKey(delAddr, delegation.GetValidatorAddr())
		if store.Has(indexKey) {
			continue
		}
		store.Set(indexKey, []byte{})
		result.IndexWritten++
	}

	store.Set(types.DelegationByValIndexReadyKey, []byte{})
	result.Elapsed = time.Since(start)
	return result, nil
}
