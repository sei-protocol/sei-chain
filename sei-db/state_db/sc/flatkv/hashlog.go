package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// Hash logger category names owned by the flatKV backend. flatKVDBHashPrefix is joined with a data DB
// directory name for that DB's per-DB LtHash (e.g. "flatKV/db/account"), or additionally with
// flatKVModHashPrefix and a module name for a per-module LtHash within that DB (e.g. "flatKV/db/account/mod/evm").
// The metadata DB is intentionally excluded — it holds no state.
const (
	FlatKVRootHashType  = "flatKV/root"
	flatKVDBHashPrefix  = "flatKV/db/"
	flatKVModHashPrefix = "mod"
)

// dbHashCategory returns the hash logger category for a single data DB's per-DB LtHash, e.g.
// dbHashCategory("account") == "flatKV/db/account".
func dbHashCategory(dir string) string {
	return flatKVDBHashPrefix + dir
}

// moduleHashCategory returns the hash logger category for a single module's per-module LtHash within a
// data DB, e.g. moduleHashCategory("account", "evm") == "flatKV/db/account/mod/evm". A module that spans
// several DBs (e.g. "evm" touches account/code/storage) reports one category per DB it actually has an
// entry in, since ModuleLtHashes is keyed per-DB, not aggregated across DBs.
func moduleHashCategory(dir, module string) string {
	return flatKVDBHashPrefix + dir + "/" + flatKVModHashPrefix + "/" + module
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
		categories = append(categories, dbHashCategory(dir))
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

		var dbHash *lthash.LtHash
		if meta != nil {
			dbHash = meta.LtHash
		}
		category := dbHashCategory(dir)
		if err := hl.ReportHash(blockNumber, category, checksumBytesOrNil(dbHash)); err != nil {
			return fmt.Errorf("failed to report flatkv db hash %q: %w", category, err)
		}

		if err := reportModuleHashes(hl, blockNumber, dir, meta); err != nil {
			return err
		}
	}
	return nil
}

// reportModuleHashes reports the checksum of every module's per-module LtHash tracked within one data DB
// (meta.ModuleLtHashes). A nil meta (DB never committed) reports nothing.
func reportModuleHashes(hl hashlog.HashLogger, blockNumber uint64, dir string, meta *ktype.LocalMeta) error {
	if meta == nil {
		return nil
	}
	for module, moduleHash := range meta.ModuleLtHashes {
		category := moduleHashCategory(dir, module)
		if err := hl.ReportHash(blockNumber, category, checksumBytesOrNil(moduleHash)); err != nil {
			return fmt.Errorf("failed to report flatkv module hash %q: %w", category, err)
		}
	}
	return nil
}

// checksumBytesOrNil returns h's checksum as a byte slice, or nil if h is nil (nothing committed yet).
// Mirrors checksumOrNil (verify.go), which renders the same "nil-tolerant checksum" for error messages
// instead of a []byte.
func checksumBytesOrNil(h *lthash.LtHash) []byte {
	if h == nil {
		return nil
	}
	checksum := h.Checksum()
	return checksum[:]
}
