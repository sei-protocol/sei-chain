package cosmos

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Compile-time check: CosmosStateStore implements db_engine.StateStore.
var _ types.StateStore = (*CosmosStateStore)(nil)

// CosmosStateStore wraps a single StateStore (MVCC DB) and satisfies db_engine.StateStore.
// It is the SS-layer adapter for the main Cosmos state (all non-EVM modules).
type CosmosStateStore struct {
	db types.StateStore
}

// NewCosmosStateStore wraps an existing StateStore as a CosmosStateStore.
func NewCosmosStateStore(db types.StateStore) types.StateStore {
	return &CosmosStateStore{db: db}
}

func (s *CosmosStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return s.db.Get(storeKey, version, key)
}

func (s *CosmosStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	return s.db.Has(storeKey, version, key)
}

func (s *CosmosStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.db.Iterator(storeKey, version, start, end)
}

func (s *CosmosStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.db.ReverseIterator(storeKey, version, start, end)
}

func (s *CosmosStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.db.RawIterate(storeKey, fn)
}

func (s *CosmosStateStore) GetLatestVersion() int64 {
	return s.db.GetLatestVersion()
}

func (s *CosmosStateStore) SetLatestVersion(version int64) error {
	return s.db.SetLatestVersion(version)
}

func (s *CosmosStateStore) GetEarliestVersion() int64 {
	return s.db.GetEarliestVersion()
}

func (s *CosmosStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.db.SetEarliestVersion(version, ignoreVersion)
}

func (s *CosmosStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.db.ApplyChangesetSync(version, changesets)
}

func (s *CosmosStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.db.ApplyChangesetAsync(version, changesets)
}

func (s *CosmosStateStore) Prune(version int64) error {
	return s.db.Prune(version)
}

func (s *CosmosStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	return s.db.Import(version, ch)
}

func (s *CosmosStateStore) Close() error {
	return s.db.Close()
}
