package hashvault

import (
	"fmt"
)

// HashVaultConfig is the configuration for a HashVault.
type HashVaultConfig struct {
	// DataDir is the directory in which the PebbleDB-backed HashVault stores its data.
	DataDir string

	// Fsync controls whether the underlying Pebble writes are fsynced.
	//
	// This field is test-only. Production callers should construct via NewPebbleHashVault, which forces
	// fsync on regardless of this value. NewUnsafePebbleHashVault honors this flag and is intended for
	// tests that exercise enough writes that fsync would dominate runtime.
	Fsync bool

	// CacheSize is the number of recent (height -> verified hash) entries in the in-process LRU cache.
	CacheSize int
}

// DefaultHashVaultConfig returns a HashVaultConfig with production defaults.
func DefaultHashVaultConfig() HashVaultConfig {
	return HashVaultConfig{
		Fsync:     false,
		CacheSize: 1024,
	}
}

// Validate returns a non-nil error if the configuration is missing required fields or has values
// that the HashVault cannot accept.
func (c *HashVaultConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data directory is required")
	}
	if c.CacheSize <= 0 {
		return fmt.Errorf("cache size must be greater than zero")
	}
	return nil
}
