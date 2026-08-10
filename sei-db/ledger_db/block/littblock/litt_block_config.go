package littblock

import (
	"fmt"
	"time"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
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
	// It is an age floor, not a retention policy: how much history this store
	// keeps is RetentionWindow below. Raising this only delays reclaiming what
	// the watermark has already released, which costs disk and buys nothing the
	// window does not already express.
	RetentionTime time.Duration

	// RetentionWindow is how much history this store keeps beyond the shared rollback
	// window of the StorageGarbageCollector that manages it, in blocks. It is what
	// gc.PrunableStore.GetRetentionWindow answers:
	//
	//	> 0  → that many blocks of history beyond the rollback window
	//	0    → keep history to serve rollback window only
	//	-1   → never prune this store (gc.InfiniteRetentionWindow)
	//
	// Zero does NOT mean "keep everything" here, unlike the KeepRecent fields on
	// StateStoreConfig and ReceiptStoreConfig, where 0 disables pruning. It is the most
	// aggressive setting this field has; "keep everything" is -1. Assigning a KeepRecent
	// value to this field inverts the retention it asks for.
	//
	// This is an input to a minimum shared across every managed store, not a policy applied
	// to this store alone: a deep window here also holds back receiptDB and the SC/SS
	// snapshots. Must be >= gc.InfiniteRetentionWindow.
	//
	// With F = LatestBlock - RollbackWindow - RetentionWindow, garbage collection guarantees:
	//
	//  1. Nothing needed to roll back to any block in
	//     [LatestBlock - RollbackWindow, LatestBlock] is deleted.
	//  2. No block or QC at or above F is deleted. So even after rolling back to
	//     LatestBlock - RollbackWindow, any of the last RetentionWindow blocks is still readable.
	//  3. Blocks and QCs below F are eventually deleted — eventually, because reclamation also
	//     waits for RetentionTime and for LittDB's own GC to come round.
	//
	// Independent of RetentionTime, which is a wall-clock TTL failsafe underneath the watermark.
	// Both must permit reclamation before any record is dropped.
	RetentionWindow int64
}

// DefaultConfig returns a BlockDBConfig preloaded with all defaults, rooted at
// dir. Override fields as needed, then pass it to NewBlockDB (which validates).
//
// RetentionWindow defaults to 0 — no extra history beyond the collector's shared rollback
// window — because it is not a blockDB-local knob. The collector prunes every store to one
// minimum, so a non-zero default here would hold receiptDB, the state WAL and the SC snapshots
// that much further back too, on blockDB's say-so. Every other store in the fleet reports 0.
// A deployment that wants deeper block history sets this at the call site, where the fleet-wide
// cost of the choice is visible.
func DefaultConfig(dir string) (*BlockDBConfig, error) {
	littConfig, err := littdb.DefaultConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to build litt config: %w", err)
	}
	return &BlockDBConfig{
		Litt:            littConfig,
		RetentionTime:   time.Hour,
		RetentionWindow: 0,
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
	if c.RetentionWindow < gc.InfiniteRetentionWindow {
		return fmt.Errorf("config.RetentionWindow must be >= %d (got %d)",
			gc.InfiniteRetentionWindow, c.RetentionWindow)
	}
	return nil
}
