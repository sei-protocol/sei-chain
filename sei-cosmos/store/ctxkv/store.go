package ctxkv

import (
	"context"
	"io"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

var _ types.KVStore = (*Store)(nil)

// Store forwards KVStore calls to parent, passing ctx into iteration so a
// historical SS MVCC skip loop can observe the caller's deadline.
type Store struct {
	parent types.KVStore
	ctx    context.Context
}

// Wrap returns parent unchanged when ctx cannot be cancelled. Otherwise it
// wraps parent so Iterator/ReverseIterator carry ctx to a ContextIterator.
func Wrap(parent types.KVStore, ctx context.Context) types.KVStore {
	if parent == nil || ctx == nil || ctx.Done() == nil {
		return parent
	}
	return &Store{parent: parent, ctx: ctx}
}

func (s *Store) GetStoreType() types.StoreType { return s.parent.GetStoreType() }
func (s *Store) GetWorkingHash() ([]byte, error) {
	return s.parent.GetWorkingHash()
}
func (s *Store) Get(key []byte) []byte { return s.parent.Get(key) }
func (s *Store) Has(key []byte) bool   { return s.parent.Has(key) }
func (s *Store) Set(key, value []byte) { s.parent.Set(key, value) }
func (s *Store) Delete(key []byte)     { s.parent.Delete(key) }
func (s *Store) CacheWrap(storeKey types.StoreKey) types.CacheWrap {
	return s.parent.CacheWrap(storeKey)
}
func (s *Store) CacheWrapWithTrace(storeKey types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return s.parent.CacheWrapWithTrace(storeKey, w, tc)
}
func (s *Store) VersionExists(version int64) bool { return s.parent.VersionExists(version) }
func (s *Store) DeleteAll(start, end []byte) error {
	return s.parent.DeleteAll(start, end)
}
func (s *Store) GetAllKeyStrsInRange(start, end []byte) []string {
	return s.parent.GetAllKeyStrsInRange(start, end)
}

func (s *Store) Iterator(start, end []byte) types.Iterator {
	return types.IteratorOn(s.parent, s.ctx, start, end, true)
}

func (s *Store) ReverseIterator(start, end []byte) types.Iterator {
	return types.IteratorOn(s.parent, s.ctx, start, end, false)
}
