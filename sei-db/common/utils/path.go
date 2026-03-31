package utils

import (
	"os"
	"path/filepath"
)

// PathExists returns true if the given path exists on disk.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetCommitStorePath returns the path for the memiavl state commitment store.
// New nodes use data/state_commit/memiavl; existing nodes with data/committer.db
// continue using the legacy path for backward compatibility.
func GetCommitStorePath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "committer.db")
	if PathExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_commit", "memiavl")
}

// GetFlatKVPath returns the path for the FlatKV EVM commit store.
// New nodes use data/state_commit/flatkv; existing nodes with data/flatkv
// continue using the legacy path for backward compatibility.
func GetFlatKVPath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "flatkv")
	if PathExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_commit", "flatkv")
}

// GetStateStorePath returns the path for the Cosmos state store (SS).
// New nodes use data/state_store/cosmos_ss/{backend}; existing nodes with
// data/{backend} continue using the legacy path for backward compatibility.
func GetStateStorePath(homePath string, backend string) string {
	legacyPath := filepath.Join(homePath, "data", backend)
	if PathExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_store", "cosmos_ss", backend)
}

// GetEVMStateStorePath returns the path for the EVM state store.
// New nodes use data/state_store/evm_ss; existing nodes with data/evm_ss
// continue using the legacy path for backward compatibility.
func GetEVMStateStorePath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "evm_ss")
	if PathExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_store", "evm_ss")
}

// GetReceiptStorePath returns the path for the receipt store.
// New nodes use data/ledger/receipt.db; existing nodes with data/receipt.db
// continue using the legacy path for backward compatibility.
func GetReceiptStorePath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "receipt.db")
	if PathExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "ledger", "receipt.db")
}

func GetChangelogPath(dbPath string) string {
	return filepath.Join(dbPath, "changelog")
}
