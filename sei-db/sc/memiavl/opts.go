package memiavl

import (
	"errors"
	"time"

	"github.com/sei-protocol/sei-db/common/logger"

	"github.com/sei-protocol/sei-db/config"
)

type Options struct {
	Dir             string
	CreateIfMissing bool
	InitialVersion  uint32
	ReadOnly        bool
	// the initial stores when initialize the empty instance
	InitialStores []string
	// keep how many snapshots
	SnapshotKeepRecent uint32
	// how often to take a snapshot
	SnapshotInterval uint32
	// Buffer size for the asynchronous commit queue, -1 means synchronous commit,
	// default to 0.
	AsyncCommitBuffer int
	// ZeroCopy if true, the get and iterator methods could return a slice pointing to mmaped blob files.
	ZeroCopy bool
	// Logger is the memiavl logger
	Logger logger.Logger

	// LoadForOverwriting if true rollbacks the state, specifically the OpenDB method will
	// truncate the versions after the `TargetVersion`, the `TargetVersion` becomes the latest version.
	// it do nothing if the target version is `0`.
	LoadForOverwriting bool

	// OnlyAllowExportOnSnapshotVersion defines whether the state sync exporter should only export the
	// version that matches wit the current memiavl snapshot version
	OnlyAllowExportOnSnapshotVersion bool

	// Limit the number of concurrent snapshot writers
	SnapshotWriterLimit int

	// Prefetch the snapshot file if amount of file in cache is below the threshold
	// Setting to <=0 means disable prefetching
	PrefetchThreshold float64

	// Minimum time interval between snapshots
	// This prevents excessive snapshot creation during catch-up. Default is 1 hour.
	SnapshotMinTimeInterval time.Duration

	// SnapshotWriteRateMBps defines the maximum write rate (MB/s) for snapshot creation.
	// This is a GLOBAL limit shared across all trees and files.
	// Default: 100. Set to a very high value (e.g., 10000) for effectively unlimited.
	// 0 or unset will use the default.
	SnapshotWriteRateMBps int
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
		opts.SnapshotInterval = config.DefaultSnapshotInterval
	}

	if opts.SnapshotWriterLimit <= 0 {
		opts.SnapshotWriterLimit = 2 // Default to 2 for lower I/O pressure on most validators
	}

	if opts.SnapshotMinTimeInterval <= 0 {
		opts.SnapshotMinTimeInterval = 1 * time.Hour
	}

	if opts.SnapshotWriteRateMBps <= 0 {
		opts.SnapshotWriteRateMBps = config.DefaultSnapshotWriteRateMBps
	}

	opts.PrefetchThreshold = 0.8
	opts.Logger = logger.NewNopLogger()
	opts.SnapshotKeepRecent = config.DefaultSnapshotKeepRecent
}
