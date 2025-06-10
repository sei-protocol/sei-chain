package memiavl

import (
	"errors"

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
	// CacheSize defines the cache's max entry size for each memiavl store.
	CacheSize int
	// LoadForOverwriting if true rollbacks the state, specifically the OpenDB method will
	// truncate the versions after the `TargetVersion`, the `TargetVersion` becomes the latest version.
	// it do nothing if the target version is `0`.
	LoadForOverwriting bool

	// OnlyAllowExportOnSnapshotVersion defines whether the state sync exporter should only export the
	// version that matches wit the current memiavl snapshot version
	OnlyAllowExportOnSnapshotVersion bool

	// Limit the number of concurrent snapshot writers
	SnapshotWriterLimit int
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
		opts.SnapshotWriterLimit = config.DefaultSnapshotWriterLimit
	}

	if opts.CacheSize < 0 {
		opts.CacheSize = config.DefaultCacheSize
	}
}
