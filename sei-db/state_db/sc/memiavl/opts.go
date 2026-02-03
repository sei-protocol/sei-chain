package memiavl

import (
	"errors"
	"runtime"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
)

// Options contains all settings for opening a memiavl database.
// It embeds Config for the configurable settings and adds runtime-specific options.
type Options struct {
	Config // Embedded config for all configurable settings

	// Dir is the directory path for the memiavl database
	Dir string
	// CreateIfMissing creates the database if it doesn't exist
	CreateIfMissing bool
	// InitialVersion is the initial version number
	InitialVersion uint32
	// ReadOnly opens the database in read-only mode
	ReadOnly bool
	// InitialStores are the initial store names when initializing an empty instance
	InitialStores []string
	// ZeroCopy if true, get and iterator methods return slices pointing to mmaped blob files
	ZeroCopy bool
	// Logger is the memiavl logger
	Logger logger.Logger
	// LoadForOverwriting if true, rollbacks the state by truncating versions after TargetVersion
	LoadForOverwriting bool
	// OnlyAllowExportOnSnapshotVersion restricts export to snapshot versions only
	OnlyAllowExportOnSnapshotVersion bool

	// snapshotMinTimeIntervalDuration is the converted Duration from Config.SnapshotMinTimeInterval
	// This is populated by FillDefaults()
	snapshotMinTimeIntervalDuration time.Duration
}

// SnapshotMinTimeDuration returns the minimum time interval between snapshots as a Duration.
// Call FillDefaults() before using this method.
func (opts *Options) SnapshotMinTimeDuration() time.Duration {
	return opts.snapshotMinTimeIntervalDuration
}

func (opts Options) Validate() error {
	if opts.ReadOnly && opts.CreateIfMissing {
		return errors.New("can't create db in read-only mode")
	}

	if opts.ReadOnly && opts.LoadForOverwriting {
		return errors.New("can't rollback db in read-only mode")
	}

	return nil
}

func (opts *Options) FillDefaults() {
	if opts.SnapshotInterval <= 0 {
		opts.SnapshotInterval = DefaultSnapshotInterval
	}

	if opts.SnapshotWriterLimit <= 0 {
		opts.SnapshotWriterLimit = runtime.NumCPU()
	}

	// Convert SnapshotMinTimeInterval (seconds) to Duration
	if opts.SnapshotMinTimeInterval > 0 {
		opts.snapshotMinTimeIntervalDuration = time.Duration(opts.SnapshotMinTimeInterval) * time.Second
	} else {
		opts.snapshotMinTimeIntervalDuration = 1 * time.Hour
	}

	if opts.SnapshotWriteRateMBps <= 0 {
		opts.SnapshotWriteRateMBps = DefaultSnapshotWriteRateMBps
	}

	if opts.SnapshotPrefetchThreshold < 0 || opts.SnapshotPrefetchThreshold > 1 {
		opts.SnapshotPrefetchThreshold = DefaultSnapshotPrefetchThreshold
	}

	if opts.Logger == nil {
		opts.Logger = logger.NewNopLogger()
	}
}
