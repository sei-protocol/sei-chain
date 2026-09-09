package composite

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// HashCategories returns the union of the live backends' hash logger categories. An absent backend
// contributes nothing, so the set tracks which backends are active (used upstream to detect when the
// logger's category set must change). Note: the memIAVL root ("memIAVL/root") is not included
// here — it is a simple-merkle aggregation owned by the cosmos layer (see MemIAVLCommitInfo).
func (cs *CompositeCommitStore) HashCategories() []string {
	var categories []string
	if cs.memIAVL != nil {
		categories = append(categories, cs.memIAVL.HashCategories()...)
	}
	if cs.flatKV != nil {
		categories = append(categories, flatkv.HashTypes()...)
	}
	return categories
}

// RecordHashes reports both backends' hashes for blockNumber. Call right after Commit.
func (cs *CompositeCommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if cs.memIAVL != nil {
		if err := cs.memIAVL.RecordHashes(hl, blockNumber); err != nil {
			return err
		}
	}
	if cs.flatKV != nil {
		// Keyed on the block cosmos committed rather than the hash's own height, which is what keeps
		// this row complete: a block whose writes never reached flatKV leaves its hash on the height
		// before, and the AppHash reports that same hash for this block.
		//nolint:gosec // commit versions are non-negative
		if err := hl.HashListener(cs.ctx, int64(blockNumber), cs.flatKVHash.Load()); err != nil {
			return fmt.Errorf("record flatkv hashes for block %d: %w", blockNumber, err)
		}
	}
	return nil
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
