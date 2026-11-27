package blockstore

import (
	"fmt"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Config configures a LittDB-backed consensus block store.
type Config struct {
	// Litt is the underlying LittDB configuration, including the data directory
	// paths. The block store builds its single table (see tableName, which holds
	// blocks and QCs both) on top of this DB. Required; use DefaultConfig to obtain
	// one with sane defaults, then override fields as needed (e.g. Litt.Fsync,
	// Litt.GCPeriod).
	Litt *littdb.Config

	// RetentionTime is the failsafe minimum age before any pruned record may be
	// reclaimed. Reclamation requires BOTH this age to elapse AND the prune
	// watermark to advance past the record. Must be positive.
	//
	// It is an age floor, not a retention policy: how much history this store keeps
	// is the RollbackWindow and LookbackWindow on config.StorageGarbageCollectorConfig.
	//
	// Default: 1h.
	RetentionTime time.Duration

	// Seed is the records of the road that the store starts at. New writes them to a store
	// that holds no record of another road. Optional.
	Seed utils.Option[Seed]
}

// DefaultConfig returns a Config preloaded with all defaults, rooted at
// dir. Override fields as needed, then pass it to New (which validates).
func DefaultConfig(dir string) (*Config, error) {
	littConfig, err := littdb.DefaultConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("littdb.DefaultConfig(): %w", err)
	}
	return &Config{
		Litt:          littConfig,
		RetentionTime: time.Hour,
	}, nil
}

// Validate performs a sanity check on the configuration.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.Litt == nil {
		return fmt.Errorf("config.Litt is required")
	}
	if c.RetentionTime <= 0 {
		return fmt.Errorf("config.RetentionTime must be positive (got %s)", c.RetentionTime)
	}
	if seed, ok := c.Seed.Get(); ok {
		if block, appQC := seed.Block.QC().Height(), seed.AppQC.Proposal().Height(); block != appQC {
			return fmt.Errorf("config.Seed holds the block of road %d and the AppQC of road %d: %w", block, appQC, types.ErrUnexpectedHeight)
		}
	}
	return nil
}
