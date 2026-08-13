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

// RecordHashes reports this store's hashes for blockNumber: the global root and each data DB's per-DB LtHash
// checksum.
//
// The hashes reported are the most recent the hasher has published, which on a committing store lags the
// block being committed. blockNumber is the caller's label for the row, so a lagging report is recorded
// against the height the caller is on rather than the height the hashes describe — the published height is
// available on the same value if that distinction ever needs to be logged.
func (s *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	published := s.PublishedHash()

	if err := hl.ReportHash(blockNumber, FlatKVRootHashType, published.Hash); err != nil {
		return fmt.Errorf("failed to report flatkv root hash: %w", err)
	}
	for _, dir := range dataDBDirs {
		category := flatKVDBHashPrefix + dir
		if err := hl.ReportHash(blockNumber, category, published.PerDBHashes[dir]); err != nil {
			return fmt.Errorf("failed to report flatkv db hash %q: %w", category, err)
		}
	}
	return nil
}
