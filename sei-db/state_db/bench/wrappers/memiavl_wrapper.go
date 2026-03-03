package wrappers

import (
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
	return m.base.Commit()
}

func (m *memIAVLWrapper) LoadVersion(version int64) error {
	_, err := m.base.LoadVersion(version, false)
	return err
}

func (m *memIAVLWrapper) Version() int64 {
	return m.base.Version()
}

func (m *memIAVLWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	return m.base.ApplyChangeSets(cs)
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
