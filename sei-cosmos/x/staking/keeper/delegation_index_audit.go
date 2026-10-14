package keeper

import (
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// TEST ONLY: the audit below exists for the mainnet shadow test; never merge it.

// DelegationByValIndexAuditInterval is the number of blocks between audits of a ready
// delegation-by-validator index.
const DelegationByValIndexAuditInterval = 10000

// delegationByValIndexAuditSampleLimit caps the bad keys an audit reports.
const delegationByValIndexAuditSampleLimit = 5

// DelegationByValIndexAudit is the result of comparing the delegation-by-validator index
// with the delegations it mirrors.
type DelegationByValIndexAudit struct {
	Delegations     int
	IndexEntries    int
	Missing         int
	Orphaned        int
	MissingSamples  []string
	OrphanedSamples []string
	Elapsed         time.Duration
}

// AuditDelegationByValIndex counts the delegations without an index entry and the index
// entries without a delegation. It reads the staking store without charging gas.
func (k Keeper) AuditDelegationByValIndex(ctx sdk.Context) DelegationByValIndexAudit {
	start := time.Now()
	store := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)).KVStore(k.storeKey)
	audit := DelegationByValIndexAudit{}

	delegations := sdk.KVStorePrefixIterator(store, types.DelegationKey)
	for ; delegations.Valid(); delegations.Next() {
		audit.Delegations++
		indexKey := delegationByValIndexKeyFromDelegationKey(delegations.Key())
		if !store.Has(indexKey) {
			audit.Missing++
			audit.MissingSamples = appendAuditSample(audit.MissingSamples, delegations.Key())
		}
	}
	_ = delegations.Close()

	entries := sdk.KVStorePrefixIterator(store, types.DelegationByValIndexKey)
	for ; entries.Valid(); entries.Next() {
		audit.IndexEntries++
		if !store.Has(types.GetDelegationKeyFromValIndexKey(entries.Key())) {
			audit.Orphaned++
			audit.OrphanedSamples = appendAuditSample(audit.OrphanedSamples, entries.Key())
		}
	}
	_ = entries.Close()

	audit.Elapsed = time.Since(start)
	return audit
}

// LogDelegationByValIndexAudit audits the delegation-by-validator index and logs the
// result, at error level when the index and the delegations disagree.
func (k Keeper) LogDelegationByValIndexAudit(ctx sdk.Context) {
	audit := k.AuditDelegationByValIndex(ctx)
	fields := []any{
		"height", ctx.BlockHeight(),
		"delegations", audit.Delegations,
		"index_entries", audit.IndexEntries,
		"missing", audit.Missing,
		"orphaned", audit.Orphaned,
		"elapsed", audit.Elapsed,
	}
	if audit.Missing == 0 && audit.Orphaned == 0 {
		logger.Info("delegation index audit passed", fields...)
		return
	}
	fields = append(fields, "missing_samples", audit.MissingSamples, "orphaned_samples", audit.OrphanedSamples)
	logger.Error("delegation index audit FAILED", fields...)
}

// delegationByValIndexKeyFromDelegationKey rearranges a delegation key,
// 0x31 | len | delAddr | len | valAddr, into its index key.
func delegationByValIndexKeyFromDelegationKey(key []byte) []byte {
	addrs := key[1:]
	delLen := int(addrs[0])
	delAddr := addrs[1 : 1+delLen]
	valAddr := addrs[2+delLen:]
	return types.GetDelegationByValIndexKey(delAddr, valAddr)
}

func appendAuditSample(samples []string, key []byte) []string {
	if len(samples) >= delegationByValIndexAuditSampleLimit {
		return samples
	}
	return append(samples, fmt.Sprintf("%X", key))
}
