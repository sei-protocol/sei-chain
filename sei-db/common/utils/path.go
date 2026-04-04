package utils

import (
	"os"
	"path/filepath"
)

// DirExists returns true if path exists and is a directory.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FileExists returns true if path exists and is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// GetCosmosSCStorePath returns the path for the memiavl state commitment store.
// New nodes use data/state_commit/memiavl; existing nodes with data/committer.db
// continue using the legacy path for backward compatibility.
func GetCosmosSCStorePath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "committer.db")
	if DirExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_commit", "memiavl")
}

// GetFlatKVPath returns the path for the FlatKV EVM commit store.
// New nodes use data/state_commit/flatkv; existing nodes with data/flatkv
// continue using the legacy path for backward compatibility.
func GetFlatKVPath(homePath string) string {
	legacyPath := filepath.Join(homePath, "data", "flatkv")
	if DirExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_commit", "flatkv")
}

// GetStateStorePath returns the path for the Cosmos state store (SS).
// New nodes use data/state_store/cosmos/{backend}; existing nodes with
// data/{backend} continue using the legacy path for backward compatibility.
func GetStateStorePath(homePath string, backend string) string {
	legacyPath := filepath.Join(homePath, "data", backend)
	if DirExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_store", "cosmos", backend)
}

// GetEVMStateStorePath returns the path for the EVM state store.
// New nodes use data/state_store/evm/{backend}; existing nodes with
// data/evm_ss continue using the legacy path for backward compatibility.
func GetEVMStateStorePath(homePath string, backend string) string {
	legacyPath := filepath.Join(homePath, "data", "evm_ss")
	if DirExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "state_store", "evm", backend)
}

// GetReceiptStorePath returns the path for the receipt store.
// New nodes use data/ledger/receipt/{backend}; existing nodes with
// data/receipt.db continue using the legacy path for backward compatibility.
func GetReceiptStorePath(homePath string, backend string) string {
	legacyPath := filepath.Join(homePath, "data", "receipt.db")
	if DirExists(legacyPath) {
		return legacyPath
	}
	return filepath.Join(homePath, "data", "ledger", "receipt", backend)
}

func GetChangelogPath(dbPath string) string {
	return filepath.Join(dbPath, "changelog")
}
