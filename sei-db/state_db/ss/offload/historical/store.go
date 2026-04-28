package historical

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// FallbackStateStore wraps a primary types.StateStore with a historical
// Reader. Get/Has reads at versions older than the primary's earliest
// version are routed to the Reader; everything else passes through
// unchanged. This is the integration point for using the offload-pipeline
// CockroachDB store as a read-fallback for pruned historical state.
//
// Iterators are NOT routed to the Reader: SQL iteration over an MVCC table
// is expressive but slow, and the trace profile is dominated by point Gets.
// A request for an iterator at a pruned version still falls back to the
// primary's behavior (typically empty results).
type FallbackStateStore struct {
	primary types.StateStore
	reader  Reader
}

var _ types.StateStore = (*FallbackStateStore)(nil)

// NewFallbackStateStore wraps primary so that Get/Has at versions <
// primary.GetEarliestVersion() consult reader instead. The wrapper takes
// ownership of both the primary and the reader for the purposes of Close.
func NewFallbackStateStore(primary types.StateStore, reader Reader) *FallbackStateStore {
	return &FallbackStateStore{primary: primary, reader: reader}
}

// shouldFallback returns true when version is strictly older than the
// primary's earliest retained version. The primary returns (nil, nil) for
// such versions today, which is indistinguishable from "key never written";
// using the version watermark gives us a deterministic split.
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
	_, err := s.reader.Get(context.Background(), storeKey, key, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
