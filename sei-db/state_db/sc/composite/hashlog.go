package composite

import (
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// HashCategories returns memIAVL's hash logger categories, or nothing when memIAVL is absent, so the
// set tracks whether it is active (used upstream to detect when the logger's category set must
// change). Note: the memIAVL root ("memIAVL/root") is not included here — it is a simple-merkle
// aggregation owned by the cosmos layer (see MemIAVLCommitInfo).
//
// flatKV is absent: this store records its hashes for the AppHash rather than logging them, so a
// flatKV column here would be one nothing reports, and a block missing a column is never written.
func (cs *CompositeCommitStore) HashCategories() []string {
	var categories []string
	if cs.memIAVL != nil {
		categories = append(categories, cs.memIAVL.HashCategories()...)
	}
	return categories
}

// RecordHashes reports memIAVL's hashes for blockNumber. Call right after Commit.
func (cs *CompositeCommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if cs.memIAVL == nil {
		return nil
	}
	return cs.memIAVL.RecordHashes(hl, blockNumber)
}

// MemIAVLCommitInfo returns the raw memIAVL commit info (its per-store hashes), or nil when memIAVL is
// not present. The cosmos layer uses it to compute the memIAVL root hash (a simple-merkle aggregation
// that requires the cosmos hashing utilities), which sei-db cannot compute on its own.
func (cs *CompositeCommitStore) MemIAVLCommitInfo() *proto.CommitInfo {
	if cs.memIAVL == nil {
		return nil
	}
	return cs.memIAVL.LastCommitInfo()
}
