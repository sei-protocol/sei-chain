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
// Version() and Commit() consult the base store's PendingVersion() so that
// benchmarks may call ApplyChangeSets multiple times (one per block) before a
// single Commit, e.g. when BlocksPerCommit > 1.
type flatKVWrapper struct {
	base flatkv.Store
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
		version = f.nextVersion()
	}
	return f.base.ApplyChangeSets(version, entry.Changesets)
}

func (f *flatKVWrapper) Commit() (int64, error) {
	version := f.base.PendingVersion()
	if version == 0 {
		version = f.base.Version() + 1
	}
	return f.base.Commit(version)
}

func (f *flatKVWrapper) LoadVersion(version int64) error {
	_, err := f.base.LoadVersion(version, false)
	return err
}

// Version returns the working version: the height stamped by the most
// recent pending ApplyChangeSets call, or committedVersion+1's predecessor
// (committedVersion) when nothing is pending.
func (f *flatKVWrapper) Version() int64 {
	if p := f.base.PendingVersion(); p != 0 {
		return p
	}
	return f.base.Version()
}

// nextVersion computes the height for the next ApplyChangeSets call: one
// past the last pending call's height, or one past the committed version
// when no writes are pending.
func (f *flatKVWrapper) nextVersion() int64 {
	if p := f.base.PendingVersion(); p != 0 {
		return p + 1
	}
	return f.base.Version() + 1
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
