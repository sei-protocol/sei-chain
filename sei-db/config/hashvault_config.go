package config

import "fmt"

// HashVaultConfig configures the hash vault, the equivocation guard over the live state DB's block hashes.
type HashVaultConfig struct {
	// DataDir is the directory the vault keeps its hashes in.
	DataDir string

	// HaltOnMismatch selects what a hash that differs from the recorded one does. When true, the state DB
	// fails and the node halts. When false, the mismatch is logged as an error, the vault discards the
	// recorded hashes from that block up, and the new hash is recorded in their place.
	HaltOnMismatch bool

	// EmptyVaultRollbackBlocks is how many blocks the state DB rewinds and replays when it opens over an
	// empty vault, so that the vault holds the hashes of recent blocks and not just the loaded one. The
	// rewind is limited to what the state commit store's snapshots and the state WAL can reach. 0 records
	// only the loaded block's hash.
	EmptyVaultRollbackBlocks uint64

	// LegacyPebbleDir is the directory of the Pebble-backed vault this one replaces, deleted when the vault
	// opens. Empty when there is none to delete.
	LegacyPebbleDir string

	// Fsync controls whether each recorded hash is fsynced before the vault reports it recorded.
	Fsync bool
}

// DefaultHashVaultConfig returns the default hash vault config. DataDir is left for the caller to set.
func DefaultHashVaultConfig() HashVaultConfig {
	return HashVaultConfig{
		HaltOnMismatch:           true,
		EmptyVaultRollbackBlocks: 1000,
		Fsync:                    true,
	}
}

// Validate returns an error if the config cannot open a vault.
func (c *HashVaultConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("hash vault data dir is required")
	}
	return nil
}
