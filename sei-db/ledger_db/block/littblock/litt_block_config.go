package littblock

import (
	"fmt"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
)

// LittBlockConfig configures a LittDB-backed types.BlockDB.
type LittBlockConfig struct {
	// Litt is the underlying LittDB configuration, including the data directory
	// paths. The block store builds its two tables (blocks, qcs) on top of this
	// DB. Required; use DefaultConfig to obtain one with sane defaults, then
	// override fields as needed (e.g. Litt.Fsync, Litt.GCPeriod).
	Litt *littdb.Config

	// Retention is the failsafe minimum age before any pruned record may be
	// reclaimed. Reclamation requires BOTH this age to elapse AND the prune
	// watermark to advance past the record, so even an over-eager watermark
	// cannot delete data younger than Retention. Must be positive.
	Retention time.Duration
}

// DefaultConfig returns a LittBlockConfig preloaded with all defaults, rooted at
// dir. Override fields as needed, then pass it to NewBlockDB (which validates).
func DefaultConfig(dir string) (*LittBlockConfig, error) {
	littConfig, err := littdb.DefaultConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to build litt config: %w", err)
	}
	return &LittBlockConfig{
		Litt:      littConfig,
		Retention: 24 * time.Hour,
	}, nil
}

// Validate performs a sanity check on the configuration.
func (c *LittBlockConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.Litt == nil {
		return fmt.Errorf("config.Litt is required")
	}
	if c.Retention <= 0 {
		return fmt.Errorf("config.Retention must be positive (got %s)", c.Retention)
	}
	return nil
}
