package littblock

import (
	"fmt"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
)

// BlockDBConfig configures a LittDB-backed types.BlockDB.
type BlockDBConfig struct {
	// Litt is the underlying LittDB configuration, including the data directory
	// paths. The block store builds its single table (see tableName, which holds
	// blocks and QCs both) on top of this DB. Required; use DefaultConfig to obtain
	// one with sane defaults, then override fields as needed (e.g. Litt.Fsync,
	// Litt.GCPeriod).
	Litt *littdb.Config

	// RetentionTime is the failsafe minimum age before any pruned record may be
	// reclaimed. Reclamation requires BOTH this age to elapse AND the prune
	// watermark to advance past the record, so even an over-eager watermark
	// cannot delete data younger than RetentionTime. Must be positive.
	//
	// It is an age floor, not a retention policy: how much history this store keeps
	// is the RollbackWindow and LookbackWindow on
	// gc.StorageGarbageCollectorConfig, which cover every managed store at once.
	// Raising this only delays reclaiming what the watermark has already released,
	// which costs disk and buys nothing those windows do not already express.
	//
	// The default is 1h (see DefaultConfig), matching the receipt store's flat TTL.
	// This is a failsafe window, not the retention itself — the watermark is
	// authoritative — so an hour is enough to notice and stop a runaway watermark
	// before reclamation acts on it. BlockDB is not yet wired to a collector, so on
	// the autobahn path (AutobahnBlockDBConfig starts from DefaultConfig) this only
	// reclaims already-released records sooner than the previous 24h; the blast
	// radius is dev/CI/devnet, where BlockDB currently runs.
	RetentionTime time.Duration
}

// DefaultConfig returns a BlockDBConfig preloaded with all defaults, rooted at
// dir. Override fields as needed, then pass it to NewBlockDB (which validates).
func DefaultConfig(dir string) (*BlockDBConfig, error) {
	littConfig, err := littdb.DefaultConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to build litt config: %w", err)
	}
	return &BlockDBConfig{
		Litt:          littConfig,
		RetentionTime: time.Hour,
	}, nil
}

// Validate performs a sanity check on the configuration.
func (c *BlockDBConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.Litt == nil {
		return fmt.Errorf("config.Litt is required")
	}
	if c.RetentionTime <= 0 {
		return fmt.Errorf("config.RetentionTime must be positive (got %s)", c.RetentionTime)
	}
	return nil
}
