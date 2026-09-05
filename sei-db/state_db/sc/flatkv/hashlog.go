package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// Hash logger category names owned by the flatKV backend. flatKVDBHashPrefix is joined with a data DB
// directory name (e.g. "flatKV/db/account").
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
// committed per-DB LtHash checksum. Call right after Commit; a blockNumber the store is not committed at
// is an error, since the hashes would then be attributed to a block they do not describe.
func (s *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	rootHash, version := s.RootHash()
	if uint64(version) != blockNumber { //nolint:gosec // commit versions are non-negative
		return fmt.Errorf("flatkv: asked to record hashes for block %d but the store is committed at %d",
			blockNumber, version)
	}
	if err := hl.ReportHash(blockNumber, FlatKVRootHashType, rootHash); err != nil {
		return fmt.Errorf("failed to report flatkv root hash: %w", err)
	}
	for _, dir := range dataDBDirs {
		var hash []byte
		if meta := s.localMeta[dir]; meta.LtHash != nil {
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
