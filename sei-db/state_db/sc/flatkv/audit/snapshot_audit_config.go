package audit

import (
	"fmt"
	"math"
	"runtime"
)

// SnapshotAuditConfig configures AuditSnapshot().
type SnapshotAuditConfig struct {
	// Root is the FlatKV root directory that holds the snapshot.
	Root string

	// Version is the version of the snapshot to audit. 0 selects the snapshot the root's current link names.
	Version uint64

	// ReadBytesPerSecond caps the key and value bytes read from the snapshot's databases per second. 0 means
	// no cap.
	ReadBytesPerSecond uint64

	// Hashers is how many goroutines hash the rows the reader hands them.
	Hashers uint32

	// BatchSize is how many rows the reader hands a hasher at a time.
	BatchSize uint32
}

// DefaultSnapshotAuditConfig returns the default audit configuration. Root has no default and must be set.
func DefaultSnapshotAuditConfig() *SnapshotAuditConfig {
	return &SnapshotAuditConfig{
		Hashers:   uint32(runtime.NumCPU()), //nolint:gosec // a CPU count fits in uint32
		BatchSize: 1024,
	}
}

// Validate reports whether this configuration can be used to run an audit.
func (c *SnapshotAuditConfig) Validate() error {
	if c.Root == "" {
		return fmt.Errorf("root must be set")
	}
	if c.Version > math.MaxInt64 {
		return fmt.Errorf("version %d exceeds the largest snapshot version %d", c.Version, int64(math.MaxInt64))
	}
	if c.Hashers == 0 {
		return fmt.Errorf("hashers must be greater than 0")
	}
	if c.BatchSize == 0 {
		return fmt.Errorf("batch size must be greater than 0")
	}
	return nil
}
