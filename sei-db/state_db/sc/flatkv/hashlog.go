package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// Hash logger category names owned by the flatKV backend. flatKVDBHashPrefix is joined with a data DB
// directory name (e.g. "flatKV/db/account"). The metadata DB is intentionally excluded — it holds only
// watermarks, not state.
const (
	FlatKVRootHashType = "flatKV/root"
	flatKVDBHashPrefix = "flatKV/db/"
)

// HashCategories returns the hash logger categories this store reports: the global flatKV root plus one
// per data DB. The set is fixed (the data DBs never change), so callers can use it to detect when the
// overall logged category set has changed.
func (s *CommitStore) HashCategories() []string {
	categories := make([]string, 0, len(dataDBDirs)+1)
	categories = append(categories, FlatKVRootHashType)
	for _, dir := range dataDBDirs {
		categories = append(categories, flatKVDBHashPrefix+dir)
	}
	return categories
}

// RecordHashes reports this store's hashes for blockNumber: the committed global root and each data DB's
// committed per-DB LtHash checksum. Intended to be called right after Commit, when localMeta holds the
// just-committed per-DB hashes and CommittedRootHash reflects the same version.
func (s *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if err := hl.ReportHash(blockNumber, FlatKVRootHashType, s.CommittedRootHash()); err != nil {
		return fmt.Errorf("failed to report flatkv root hash: %w", err)
	}
	for _, dir := range dataDBDirs {
		var hash []byte
		if meta := s.localMeta[dir]; meta != nil && meta.LtHash != nil {
			checksum := meta.LtHash.Checksum()
			hash = checksum[:]
		}
		category := flatKVDBHashPrefix + dir
		if err := hl.ReportHash(blockNumber, category, hash); err != nil {
			return fmt.Errorf("failed to report flatkv db hash %q: %w", category, err)
		}
	}
	return nil
}
