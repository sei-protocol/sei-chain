package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*flatKVWrapper)(nil)

// flatKVWrapper wraps a flatkv commit store to implement the DBWrapper interface.
// Version() returns the working version (committedVersion+1) when there are pending
// changes, matching memiavl's WorkingCommitInfo().Version semantics for benchmarks.
type flatKVWrapper struct {
	base           flatkv.Store
	hasPending     bool
	pendingVersion int64
}

// NewFlatKVWrapper creates a new flatKVWrapper with a given flatkv store.
func NewFlatKVWrapper(store flatkv.Store) DBWrapper {
	return &flatKVWrapper{
		base: store,
	}
}

func (f *flatKVWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	version := entry.Version
	if version <= 0 {
		version = f.base.Version() + 1
	}
	err := f.base.ApplyChangeSets(version, entry.Changesets)
	if err == nil {
		f.hasPending = true
		f.pendingVersion = version
	}
	return err
}

func (f *flatKVWrapper) Commit() (int64, error) {
	version := f.pendingVersion
	if version == 0 {
		version = f.base.Version() + 1
	}
	committed, err := f.base.Commit(version)
	if err == nil {
		f.hasPending = false
		f.pendingVersion = 0
	}
	return committed, err
}

func (f *flatKVWrapper) LoadVersion(version int64) error {
	_, err := f.base.LoadVersion(version, false)
	return err
}

func (f *flatKVWrapper) Version() int64 {
	if f.hasPending {
		return f.base.Version() + 1
	}
	return f.base.Version()
}

func (f *flatKVWrapper) Importer(version int64) (types.Importer, error) {
	return f.base.Importer(version)
}

func (f *flatKVWrapper) Close() error {
	return f.base.Close()
}

func (f *flatKVWrapper) Read(key []byte) (data []byte, found bool, err error) {
	val, ok := f.base.Get(keys.EVMStoreKey, key)
	return val, ok, nil
}

func (f *flatKVWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return f.base.GetPhaseTimer()
}
