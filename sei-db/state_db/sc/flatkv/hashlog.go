package flatkv

import "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"

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
	return hashCategories()
}

// hashCategories returns the same set without needing a store, for a caller that must open the logger
// before the store that reports to it.
func hashCategories() []string {
	categories := make([]string, 0, len(dataDBDirs)+1)
	categories = append(categories, FlatKVRootHashType)
	for _, dir := range dataDBDirs {
		categories = append(categories, flatKVDBHashPrefix+dir)
	}
	return categories
}

// Reports one block's hashes: the global root and each data database's per-DB checksum, under the
// height the hash describes rather than the height being committed.
//
// Runs on the finalization goroutine, so a hash reaches the log without the commit path waiting for
// hashing to catch up. Failures are logged and stop further reporting: the log is diagnostic, and a
// logger closed underneath this manager would otherwise complain once per block forever.
func (fm *FinalizationManager) reportHashes(hash *lthash.BlockHash) {
	if fm.reportingFailed {
		return
	}
	blockNumber := uint64(hash.BlockNumber) //nolint:gosec // commit versions are non-negative

	rootHash := hash.Global.Checksum()
	if err := fm.hashLogger.ReportHash(blockNumber, FlatKVRootHashType, rootHash[:]); err != nil {
		fm.reportingFailed = true
		logger.Error("stopped reporting flatkv hashes", "block", blockNumber, "err", err)
		return
	}
	for _, dir := range dataDBDirs {
		var dbChecksum []byte
		if dbHash := hash.PerDB[dir]; dbHash != nil {
			checksum := dbHash.Checksum()
			dbChecksum = checksum[:]
		}
		category := flatKVDBHashPrefix + dir
		if err := fm.hashLogger.ReportHash(blockNumber, category, dbChecksum); err != nil {
			fm.reportingFailed = true
			logger.Error("stopped reporting flatkv hashes", "block", blockNumber, "category", category, "err", err)
			return
		}
	}
}
