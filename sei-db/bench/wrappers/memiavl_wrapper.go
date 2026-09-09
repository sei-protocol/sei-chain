package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*memIAVLWrapper)(nil)

// A light wrapper around a memiavl commit store to implement the DBWrapper interface.
type memIAVLWrapper struct {
	base *memiavl.CommitStore
}

// NewMemIAVLWrapper creates a new memIAVLWrapper with a given memiavl commit store.
func NewMemIAVLWrapper(commitStore *memiavl.CommitStore) DBWrapper {
	return &memIAVLWrapper{
		base: commitStore,
	}
}

func (m *memIAVLWrapper) Commit() (int64, error) {
	// The benchmark wrapper interface carries no height, so the next one is derived here. That is
	// sound only because nothing in the benchmark path takes a block's hash before committing it.
	return m.base.Commit(m.base.Version() + 1)
}

func (m *memIAVLWrapper) LoadLatest() error {
	// memiavl's Committer signature is pinned; (0, false) is its load-latest-writable path.
	_, err := m.base.LoadVersion(0, false)
	return err
}

func (m *memIAVLWrapper) Version() int64 {
	return m.base.Version()
}

func (m *memIAVLWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	return m.base.ApplyChangeSets(entry.Changesets)
}

func (m *memIAVLWrapper) Importer(version int64) (types.Importer, error) {
	// Close DB first to release lock
	if err := m.Close(); err != nil {
		return nil, err
	}
	return m.base.Importer(version)
}

func (m *memIAVLWrapper) Close() error {
	return m.base.Close()
}

func (m *memIAVLWrapper) Read(key []byte) (data []byte, found bool, err error) {
	store := m.base.GetChildStoreByName(EVMStoreName)
	data = store.Get(key)
	return data, data != nil, nil
}

func (m *memIAVLWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}
