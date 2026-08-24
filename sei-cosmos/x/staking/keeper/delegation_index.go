package keeper

import (
	"time"

	"golang.org/x/mod/semver"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// DelegationByValIndexUpgrade is the upgrade at which SetDelegation and
// RemoveDelegation begin maintaining the validator-indexed delegation store.
const DelegationByValIndexUpgrade = "v6.7"

// delegationByValIndexActive reports whether SetDelegation and RemoveDelegation
// should maintain the validator-indexed delegation store.
//
// Live execution always does: the current binary only ever executes at/after
// the upgrade that ships this behavior, so a non-tracing block is never a
// pre-upgrade block. The only place pre-upgrade behavior must be reproduced is
// when re-tracing a historical block, where the era is signaled through
// ClosestUpgradeName (see app.RPCContextProvider).
func delegationByValIndexActive(ctx sdk.Context) bool {
	if !ctx.IsTracing() {
		return true
	}
	return semver.Compare(ctx.ClosestUpgradeName(), DelegationByValIndexUpgrade) >= 0
}

// BackfillDelegationByValIndexResult reports the outcome of a delegation index backfill.
type BackfillDelegationByValIndexResult struct {
	TotalDelegations int
	IndexWritten     int
	AlreadyIndexed   int
	DryRun           bool
	Elapsed          time.Duration
}

// BackfillDelegationByValIndex writes validator-indexed delegation keys for existing
// delegations. When dryRun is true, delegations are counted but no store writes occur.
func (k Keeper) BackfillDelegationByValIndex(ctx sdk.Context, dryRun bool) BackfillDelegationByValIndexResult {
	start := time.Now()
	result := BackfillDelegationByValIndexResult{DryRun: dryRun}

	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		delegation := types.MustUnmarshalDelegation(k.cdc, iterator.Value())
		result.TotalDelegations++

		delegatorAddress := sdk.MustAccAddressFromBech32(delegation.DelegatorAddress)
		valAddr := delegation.GetValidatorAddr()
		indexKey := types.GetDelegationByValIndexKey(delegatorAddress, valAddr)
		if store.Has(indexKey) {
			result.AlreadyIndexed++
			continue
		}

		if !dryRun {
			store.Set(indexKey, []byte{})
		}
		result.IndexWritten++
	}

	result.Elapsed = time.Since(start)
	return result
}
