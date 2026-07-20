package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// Hash logger category names owned by the flatKV backend. flatKVDBHashPrefix is joined with a data DB
// directory name (e.g. "flatKV/db/account"). flatKVModHashInfix additionally joins a module name within
// that DB (e.g. "flatKV/db/account/mod/evm"), mirroring memIAVL's "<backend>/mod/<module>" convention.
// The metadata DB is intentionally excluded — it holds only watermarks, not state.
const (
	FlatKVRootHashType = "flatKV/root"
	flatKVDBHashPrefix = "flatKV/db/"
	flatKVModHashInfix = "/mod/"
)

// moduleHashCategory returns the hash logger category for a single module's per-module LtHash within a
// data DB, e.g. moduleHashCategory("account", "evm") == "flatKV/db/account/mod/evm". A module that spans
// several DBs (e.g. "evm" touches account/code/storage) reports one category per DB it actually has an
// entry in, since ModuleLtHashes is keyed per-DB, not aggregated across DBs.
func moduleHashCategory(dir, module string) string {
	return flatKVDBHashPrefix + dir + flatKVModHashInfix + module
}

// HashCategories returns the hash logger categories this store reports: the global flatKV root, one per
// data DB, and one per (data DB, module) pair currently tracked in that DB's LocalMeta.ModuleLtHashes.
// The per-DB set is fixed (the data DBs never change), but the per-module set is dynamic — it grows as
// new modules first write into a DB — so callers must recompute this every block to detect changes
// (handled upstream by reopening/rotating the logger when the set changes).
func (s *CommitStore) HashCategories() []string {
	categories := make([]string, 0, len(dataDBDirs)+1)
	categories = append(categories, FlatKVRootHashType)
	for _, dir := range dataDBDirs {
		categories = append(categories, flatKVDBHashPrefix+dir)
		if meta := s.localMeta[dir]; meta != nil {
			for module := range meta.ModuleLtHashes {
				categories = append(categories, moduleHashCategory(dir, module))
			}
		}
	}
	return categories
}

// RecordHashes reports this store's hashes for blockNumber: the committed global root, each data DB's
// committed per-DB LtHash checksum, and each (data DB, module) pair's committed per-module LtHash
// checksum. Intended to be called right after Commit, when localMeta holds the just-committed per-DB and
// per-module hashes and CommittedRootHash reflects the same version.
func (s *CommitStore) RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error {
	if err := hl.ReportHash(blockNumber, FlatKVRootHashType, s.CommittedRootHash()); err != nil {
		return fmt.Errorf("failed to report flatkv root hash: %w", err)
	}
	for _, dir := range dataDBDirs {
		meta := s.localMeta[dir]

		var hash []byte
		if meta != nil && meta.LtHash != nil {
			checksum := meta.LtHash.Checksum()
			hash = checksum[:]
		}
		category := flatKVDBHashPrefix + dir
		if err := hl.ReportHash(blockNumber, category, hash); err != nil {
			return fmt.Errorf("failed to report flatkv db hash %q: %w", category, err)
		}

		if meta == nil {
			continue
		}
		for module, moduleHash := range meta.ModuleLtHashes {
			var moduleHashBytes []byte
			if moduleHash != nil {
				checksum := moduleHash.Checksum()
				moduleHashBytes = checksum[:]
			}
			modCategory := moduleHashCategory(dir, module)
			if err := hl.ReportHash(blockNumber, modCategory, moduleHashBytes); err != nil {
				return fmt.Errorf("failed to report flatkv module hash %q: %w", modCategory, err)
			}
		}
	}
	return nil
}
