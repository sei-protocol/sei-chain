package memiavl

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// memIAVLModHashPrefix is joined with a store/module name to form that module's hash logger category
// (e.g. "memIAVL/mod/bank"). The memIAVL root hash ("memIAVL/root") is a simple-merkle aggregation over
// these per-module hashes and is reported by the cosmos layer (rootmulti), which owns the merkle
// computation; this backend reports only the per-module hashes it computes natively.
const memIAVLModHashPrefix = "memIAVL/mod/"

// HashCategories returns one category per module currently in the tree. The set is dynamic: it is empty
// on a fresh (genesis) store and grows/shrinks as modules are added/removed, so the overall logged set
// changes over time (handled upstream by reopening the logger when the set changes).
func (cs *CommitStore) HashCategories() []string {
	if cs == nil || cs.db == nil {
		return nil
	}
	commitInfo := cs.db.LastCommitInfo()
	if commitInfo == nil {
		return nil
	}
	categories := make([]string, 0, len(commitInfo.StoreInfos))
	for _, storeInfo := range commitInfo.StoreInfos {
		categories = append(categories, memIAVLModHashPrefix+storeInfo.Name)
	}
	return categories
}

// RecordHashes reports each module's committed root hash for blockNumber. Intended to be called right
// after Commit, when LastCommitInfo reflects the just-committed version.
func (cs *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if cs == nil || cs.db == nil {
		return nil
	}
	commitInfo := cs.db.LastCommitInfo()
	if commitInfo == nil {
		return nil
	}
	for _, storeInfo := range commitInfo.StoreInfos {
		category := memIAVLModHashPrefix + storeInfo.Name
		// Copy: the logger retains the slice and reads it asynchronously, while the next commit replaces
		// the commit info's hashes.
		hash := append([]byte(nil), storeInfo.CommitId.Hash...)
		if err := hl.ReportHash(blockNumber, category, hash); err != nil {
			return fmt.Errorf("failed to report memiavl mod hash %q: %w", category, err)
		}
	}
	return nil
}
