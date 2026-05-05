package historical

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// FallbackStateStore routes Get/Has below the primary's earliest version to
// the Reader; iterators and writes always go to the primary.
type FallbackStateStore struct {
	primary types.StateStore
	reader  Reader
}

var _ types.StateStore = (*FallbackStateStore)(nil)

// NewFallbackStateStore takes ownership of primary and reader for Close.
func NewFallbackStateStore(primary types.StateStore, reader Reader) *FallbackStateStore {
	return &FallbackStateStore{primary: primary, reader: reader}
}

func (s *FallbackStateStore) shouldFallback(version int64) bool {
	earliest := s.primary.GetEarliestVersion()
	return earliest > 0 && version < earliest
}

func (s *FallbackStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if !s.shouldFallback(version) {
		return s.primary.Get(storeKey, version, key)
	}
	v, err := s.reader.Get(context.Background(), storeKey, key, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return v.Bytes, nil
}

func (s *FallbackStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	if !s.shouldFallback(version) {
		return s.primary.Has(storeKey, version, key)
	}
	return s.reader.Has(context.Background(), storeKey, key, version)
}

func (s *FallbackStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.primary.Iterator(storeKey, version, start, end)
}

func (s *FallbackStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.primary.ReverseIterator(storeKey, version, start, end)
}

func (s *FallbackStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.primary.RawIterate(storeKey, fn)
}

func (s *FallbackStateStore) GetLatestVersion() int64 { return s.primary.GetLatestVersion() }

func (s *FallbackStateStore) SetLatestVersion(version int64) error {
	return s.primary.SetLatestVersion(version)
}

func (s *FallbackStateStore) GetEarliestVersion() int64 { return s.primary.GetEarliestVersion() }

func (s *FallbackStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.primary.SetEarliestVersion(version, ignoreVersion)
}

func (s *FallbackStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.primary.ApplyChangesetSync(version, changesets)
}

func (s *FallbackStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.primary.ApplyChangesetAsync(version, changesets)
}

func (s *FallbackStateStore) Prune(version int64) error { return s.primary.Prune(version) }

func (s *FallbackStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	return s.primary.Import(version, ch)
}

func (s *FallbackStateStore) Close() error {
	primaryErr := s.primary.Close()
	readerErr := s.reader.Close()
	if primaryErr != nil {
		return primaryErr
	}
	return readerErr
}
