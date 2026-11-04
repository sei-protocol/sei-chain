package builder

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// AppHashBuilderConfig configures a StandardAppHashBuilder.
type AppHashBuilderConfig struct {

	// The EVM chain ID committed to by every app hash.
	ChainID uint64

	// The giga activation height: the first block with an app hash, whose previous app hash is all zeros. Inputs
	// for blocks below it are discarded. Must be the same on every node of the chain.
	GigaActivationHeight uint64

	// The number of calls that may wait for the builder before callers block. Higher values absorb longer stalls,
	// at the cost of memory.
	InputBufferSize int

	// The most requests the builder runs before storing the app hashes of the blocks they complete. Higher values
	// amortize each store over more blocks, at the cost of latency.
	MaxBatchSize int

	// The number of newest app hashes kept in memory for reads.
	RecentAppHashCacheSize int

	// How long a read cursor may go without serving a read before it is closed.
	ReadCursorIdleTimeout time.Duration

	// Configures the WAL holding the app hashes. Its Path is the hash vault's directory, and its PermitGaps is
	// ignored. App hashes survive a power loss only if its FsyncOnFlush is true.
	HashVaultConfig seiwal.Config

	// Whether the builder writes each app hash it builds to a hash log, as a debugging aid.
	HashLogEnabled bool

	// Configures the hash log. Read only if HashLogEnabled. Its Path and Version have no default and must be set,
	// and its Path must not be shared with another hash logger. Only the fields a hashlog.HashLogWriter reads
	// apply.
	HashLogConfig hashlog.HashLoggerConfig
}

// Returns an AppHashBuilderConfig holding every default. HashVaultConfig.Path has no default and must be set.
func DefaultAppHashBuilderConfig() AppHashBuilderConfig {
	hashVault := seiwal.DefaultConfig("", "app_hash_vault")
	hashVault.FsyncOnFlush = true
	return AppHashBuilderConfig{
		ChainID:                0,
		GigaActivationHeight:   1,
		InputBufferSize:        1024,
		MaxBatchSize:           1024,
		RecentAppHashCacheSize: 1024,
		ReadCursorIdleTimeout:  60 * time.Second,
		HashVaultConfig:        *hashVault,
		HashLogEnabled:         false,
		HashLogConfig:          *hashlog.DefaultHashLoggerConfig("", ""),
	}
}

// Returns an error if the configuration is invalid.
func (c *AppHashBuilderConfig) Validate() error {
	if c.GigaActivationHeight == 0 {
		return fmt.Errorf("giga activation height must be at least 1")
	}
	if c.InputBufferSize <= 0 {
		return fmt.Errorf("input buffer size must be positive, got %d", c.InputBufferSize)
	}
	if c.MaxBatchSize <= 0 {
		return fmt.Errorf("max batch size must be positive, got %d", c.MaxBatchSize)
	}
	if c.RecentAppHashCacheSize <= 0 {
		return fmt.Errorf("recent app hash cache size must be positive, got %d", c.RecentAppHashCacheSize)
	}
	if c.ReadCursorIdleTimeout <= 0 {
		return fmt.Errorf("read cursor idle timeout must be positive, got %s", c.ReadCursorIdleTimeout)
	}
	if err := c.HashVaultConfig.Validate(); err != nil {
		return fmt.Errorf("invalid hash vault config: %w", err)
	}
	return nil
}
