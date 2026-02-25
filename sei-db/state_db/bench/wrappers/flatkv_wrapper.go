package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*flatKVWrapper)(nil)

// flatKVWrapper wraps a flatkv commit store to implement the DBWrapper interface.
// Version() returns the working version (committedVersion+1) when there are pending
// changes, matching memiavl's WorkingCommitInfo().Version semantics for benchmarks.
type flatKVWrapper struct {
	base       flatkv.Store
	hasPending bool
}

// NewFlatKVWrapper creates a new flatKVWrapper with a given flatkv store.
func NewFlatKVWrapper(store flatkv.Store) DBWrapper {
	return &flatKVWrapper{
		base: store,
	}
}

func (f *flatKVWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	err := f.base.ApplyChangeSets(cs)
	if err == nil {
		f.hasPending = true
	}
	return err
}

func (f *flatKVWrapper) Commit() (int64, error) {
	version, err := f.base.Commit()
	if err == nil {
		f.hasPending = false
	}
	return version, err
}

func (f *flatKVWrapper) LoadVersion(version int64) error {
	_, err := f.base.LoadVersion(version)
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
