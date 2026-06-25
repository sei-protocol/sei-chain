package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// FlatKVRootHashType is the hash logger category for the flatKV global root hash.
//
// NOTE (v6.5.0 base): v6.5.0's flatKV tracks only a single global LtHash; per-DB LtHash tracking was
// added later upstream (#3074). The upstream hashlog integration also reports one category per data DB
// ("flatKV/db/<name>"), but those are omitted here because this base cannot compute per-DB hashes.
const FlatKVRootHashType = "flatKV/root"

// HashCategories returns the hash logger categories this store reports. On this base that is just the
// global flatKV root (see the note on FlatKVRootHashType).
func (s *CommitStore) HashCategories() []string {
	return []string{FlatKVRootHashType}
}

// RecordHashes reports the committed global flatKV root hash for blockNumber. Intended to be called
// right after Commit, when CommittedRootHash reflects the just-committed version.
func (s *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if err := hl.ReportHash(blockNumber, FlatKVRootHashType, s.CommittedRootHash()); err != nil {
		return fmt.Errorf("failed to report flatkv root hash: %w", err)
	}
	return nil
}
