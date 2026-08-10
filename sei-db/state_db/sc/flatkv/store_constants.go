package flatkv

import "errors"

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
	metadataDir  = "metadata"

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
