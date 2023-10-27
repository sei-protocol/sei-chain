package memiavl

import (
	"errors"
	"github.com/sei-protocol/sei-db/proto"

	"github.com/sei-protocol/sei-db/common/logger"
)

type Options struct {
	Logger          logger.Logger
	CreateIfMissing bool
	InitialVersion  uint32
	ReadOnly        bool
	// the initial stores when initialize the empty instance
	InitialStores          []string
	SnapshotKeepRecent     uint32
	SnapshotInterval       uint32
	TriggerStateSyncExport func(height int64)
	CommitInterceptor      func(version int64, changesets []*proto.NamedChangeSet) error
	// load the target version instead of latest version
	TargetVersion uint32
	// Buffer size for the asynchronous commit queue, -1 means synchronous commit,
	// default to 0.
	AsyncCommitBuffer int
	// ZeroCopy if true, the get and iterator methods could return a slice pointing to mmaped blob files.
	ZeroCopy bool
	// CacheSize defines the cache's max entry size for each memiavl store.
	CacheSize int
	// LoadForOverwriting if true rollbacks the state, specifically the Load method will
	// truncate the versions after the `TargetVersion`, the `TargetVersion` becomes the latest version.
	// it do nothing if the target version is `0`.
	LoadForOverwriting bool

	// Limit the number of concurrent snapshot writers
	SnapshotWriterLimit int

	// SDK46Compatible defines if the root hash is compatible with cosmos-sdk 0.46 and before.
	SdkBackwardCompatible bool

	// ExportNonSnapshotVersion if true, the state snapshot can be exported at any version
	ExportNonSnapshotVersion bool
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
	if opts.Logger == nil {
		opts.Logger = logger.NewNopLogger()
	}

	if opts.SnapshotInterval == 0 {
		opts.SnapshotInterval = DefaultSnapshotInterval
	}

	if opts.SnapshotWriterLimit <= 0 {
		opts.SnapshotWriterLimit = DefaultSnapshotWriterLimit
	}
}
