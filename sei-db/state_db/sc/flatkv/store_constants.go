package flatkv

import (
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

const (
	// Top-level directory names
	flatkvRootDir = "flatkv"
	changelogDir  = "changelog"
	lockFileName  = "LOCK"

	// DB subdirectories (inside each snapshot)
	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	miscDBDir    = "misc"

	// Suffixes for atomic directory operations
	tmpSuffix      = "-tmp"
	removingSuffix = "-removing"

	readOnlyDirPrefix = "readonly-"

	flatkvMeterName = "seidb_flatkv"
)

// dataDBDirs lists all data DB directory names (used for per-DB LtHash iteration).
var dataDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, miscDBDir}

// errReadOnly is returned by every method that would modify a store opened read-only.
var errReadOnly = errors.New("flatkv: store is read-only")

// HashTypes returns the hash log columns this store's hashes are recorded under: the store-wide
// root, and one per data database. A node declares them when it constructs the hash logger
// (hashlog.HashLoggerConfig.HashTypes), which is the only place a column is registered.
//
// The set is fixed, because what flatKV holds is known.
func HashTypes() []string {
	hashTypes := make([]string, 0, len(dataDBDirs)+1)
	hashTypes = append(hashTypes, hashlog.FlatKVRootHashType)
	for _, dataDB := range dataDBDirs {
		hashTypes = append(hashTypes, hashlog.FlatKVDBHashPrefix+dataDB)
	}
	return hashTypes
}
