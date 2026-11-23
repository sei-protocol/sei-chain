package keeper

import (
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// DelegationByValIndexBackfillBatchSize is the number of delegations the staking
// EndBlocker indexes per block while the delegation-by-validator index is backfilled.
const DelegationByValIndexBackfillBatchSize = 1000

// MigrateDelegationByValIndexResult reports the outcome of populating the
// delegation-by-validator index.
type MigrateDelegationByValIndexResult struct {
	TotalDelegations int
	AlreadyReady     bool
	Elapsed          time.Duration
}

// DelegationByValIndexReady reports whether the delegation-by-validator index is
// populated in the state this context reads. It consumes no gas.
func (k Keeper) DelegationByValIndexReady(ctx sdk.Context) bool {
	return k.hasGasless(ctx, types.DelegationByValIndexReadyKey)
}

// delegationByValIndexMaintained reports whether delegation writes keep the
// delegation-by-validator index current: the index is ready, or its backfill has started.
// It consumes no gas.
func (k Keeper) delegationByValIndexMaintained(ctx sdk.Context) bool {
	return k.DelegationByValIndexReady(ctx) || k.hasGasless(ctx, types.DelegationByValIndexCursorKey)
}

// hasGasless reports whether key is in the staking store without charging for the read.
func (k Keeper) hasGasless(ctx sdk.Context, key []byte) bool {
	// Delegation writes check the index markers at every height, so charging for the read
	// would change the gas of pre-upgrade transactions that touch a delegation.
	gasless := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))
	return gasless.KVStore(k.storeKey).Has(key)
}

// StartDelegationByValIndexBackfill begins populating the delegation-by-validator
// index across blocks. From this call on, delegation writes maintain the index, and
// BackfillDelegationByValIndex indexes the delegations stored before it. It is a no-op
// once the index is ready or its backfill has started.
func (k Keeper) StartDelegationByValIndexBackfill(ctx sdk.Context) {
	if k.delegationByValIndexMaintained(ctx) {
		return
	}
	ctx.KVStore(k.storeKey).Set(types.DelegationByValIndexCursorKey, []byte{})
}

// BackfillDelegationByValIndex indexes up to limit stored delegations that follow the
// backfill cursor, and marks the index ready once every delegation is indexed. It
// returns the number of delegations indexed, and is a no-op when no backfill is in
// progress. limit must be positive.
func (k Keeper) BackfillDelegationByValIndex(ctx sdk.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("delegation-by-validator index backfill limit must be positive, got %d", limit)
	}
	store := ctx.KVStore(k.storeKey)
	cursor := store.Get(types.DelegationByValIndexCursorKey)
	if cursor == nil {
		return 0, nil
	}

	start := types.DelegationKey
	if len(cursor) > 0 {
		// The cursor holds the last delegation key indexed; the next batch starts just after it.
		start = append(append([]byte{}, cursor...), 0x00)
	}
	iterator := store.Iterator(start, sdk.PrefixEndBytes(types.DelegationKey))
	defer func() { _ = iterator.Close() }()

	indexed := 0
	var last []byte
	for ; iterator.Valid() && indexed < limit; iterator.Next() {
		if err := k.indexStoredDelegation(store, iterator.Key(), iterator.Value()); err != nil {
			return indexed, err
		}
		last = append([]byte{}, iterator.Key()...)
		indexed++
	}

	if iterator.Valid() {
		store.Set(types.DelegationByValIndexCursorKey, last)
		return indexed, nil
	}
	store.Delete(types.DelegationByValIndexCursorKey)
	store.Set(types.DelegationByValIndexReadyKey, []byte{})
	return indexed, nil
}

// MigrateDelegationByValIndex writes an index entry for every stored delegation in one
// pass and marks the index ready. Its cost grows with the number of delegations, so it
// is for genesis only; an upgrade starts a paced backfill with
// StartDelegationByValIndexBackfill instead. It is a no-op once the index is ready.
func (k Keeper) MigrateDelegationByValIndex(ctx sdk.Context) (MigrateDelegationByValIndexResult, error) {
	start := time.Now()
	store := ctx.KVStore(k.storeKey)

	if k.DelegationByValIndexReady(ctx) {
		return MigrateDelegationByValIndexResult{AlreadyReady: true, Elapsed: time.Since(start)}, nil
	}

	result := MigrateDelegationByValIndexResult{}
	iterator := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		if err := k.indexStoredDelegation(store, iterator.Key(), iterator.Value()); err != nil {
			return result, err
		}
		result.TotalDelegations++
	}

	store.Delete(types.DelegationByValIndexCursorKey)
	store.Set(types.DelegationByValIndexReadyKey, []byte{})
	result.Elapsed = time.Since(start)
	return result, nil
}

// indexStoredDelegation writes the index entry for the delegation stored at key.
func (k Keeper) indexStoredDelegation(store sdk.KVStore, key, value []byte) error {
	delegation, err := types.UnmarshalDelegation(k.cdc, value)
	if err != nil {
		return fmt.Errorf("unmarshal delegation at key %X: %w", key, err)
	}
	delAddr, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
	if err != nil {
		return fmt.Errorf("parse delegator address %q: %w", delegation.DelegatorAddress, err)
	}
	store.Set(types.GetDelegationByValIndexKey(delAddr, delegation.GetValidatorAddr()), []byte{})
	return nil
}
