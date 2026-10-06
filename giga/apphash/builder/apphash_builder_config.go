package builder

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// AppHashBuilderConfig configures a StandardAppHashBuilder.
type AppHashBuilderConfig struct {

	// The number of calls that may wait for the builder before callers block. Higher values absorb longer stalls,
	// at the cost of memory.
	InputBufferSize int

	// Configures the WAL holding the app hashes. Its Path is the hash vault's directory, and its PermitGaps is
	// ignored.
	HashVault seiwal.Config
}

// Returns an AppHashBuilderConfig holding every default. HashVault.Path has no default and must be set.
func DefaultAppHashBuilderConfig() AppHashBuilderConfig {
	hashVault := seiwal.DefaultConfig("", "app_hash_vault")
	hashVault.FsyncOnFlush = true
	return AppHashBuilderConfig{
		InputBufferSize: 1024,
		HashVault:       *hashVault,
	}
}

// Returns an error if the configuration is invalid.
func (c *AppHashBuilderConfig) Validate() error {
	if c.InputBufferSize <= 0 {
		return fmt.Errorf("input buffer size must be positive, got %d", c.InputBufferSize)
	}
	if err := c.HashVault.Validate(); err != nil {
		return fmt.Errorf("invalid hash vault config: %w", err)
	}
	return nil
}
