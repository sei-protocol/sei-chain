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
// FlatKV persists exactly one block per Commit, so benchmarks must commit every
// block. Several
// ApplyChangeSets calls may still precede one Commit as long as they all target the
// same height; Commit() consults PendingVersion() to find that height.
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

func (f *flatKVWrapper) LoadVersion(_ int64) error {
	return f.base.LoadLatest()
}

func (f *flatKVWrapper) Version() int64 {
	return f.base.Version()
}

// nextVersion computes the height for the next ApplyChangeSets call: one past the
// committed version. It deliberately ignores PendingVersion() — a pending block's
// writes may be extended at its own height, never continued at the next one.
func (f *flatKVWrapper) nextVersion() int64 {
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
