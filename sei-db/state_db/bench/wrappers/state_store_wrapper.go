package wrappers

import (
	"fmt"
	"sync/atomic"

	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*stateStoreWrapper)(nil)

// stateStoreWrapper adapts a versioned StateStore (SS layer) to the DBWrapper
// interface used by the cryptosim benchmark. Each ApplyChangeSets call maps to
// a single ApplyChangesetSync at an incrementing version. The SS layer persists
// on every apply, so Commit is a no-op.
type stateStoreWrapper struct {
	base    dbTypes.StateStore
	version atomic.Int64
}

func NewStateStoreWrapper(store dbTypes.StateStore) DBWrapper {
	w := &stateStoreWrapper{
		base: store,
	}
	w.version.Store(store.GetLatestVersion())
	return w
}

func (s *stateStoreWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	nextVersion := s.version.Add(1)
	return s.base.ApplyChangesetSync(nextVersion, cs)
}

func (s *stateStoreWrapper) Read(key []byte) (data []byte, found bool, err error) {
	version := s.version.Load()
	if version == 0 {
		return nil, false, nil
	}
	val, err := s.base.Get(EVMStoreName, version, key)
	if err != nil {
		return nil, false, err
	}
	return val, val != nil, nil
}

func (s *stateStoreWrapper) Commit() (int64, error) {
	return s.version.Load(), nil
}

func (s *stateStoreWrapper) Close() error {
	return s.base.Close()
}

func (s *stateStoreWrapper) Version() int64 {
	return s.version.Load()
}

func (s *stateStoreWrapper) LoadVersion(_ int64) error {
	return nil
}

func (s *stateStoreWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, fmt.Errorf("import not supported for state store wrapper")
}
