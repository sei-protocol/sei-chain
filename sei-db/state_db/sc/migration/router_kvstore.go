package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	db "github.com/tendermint/tm-db"
)

// rootHashSize matches the digest length used by the other CommitKVStore
// implementations in this codebase: memiavl returns sha256 (32 B) and flatkv
// returns Blake3-256 (32 B).
const rootHashSize = 32

var _ types.CommitKVStore = (*RouterCommitKVStore)(nil)

// RouterCommitKVStore adapts a [Router] (which is keyed by store name on every
// call) to the store-name-less [types.CommitKVStore] interface by binding the
// view to a single module store name.
//
// The CommitKVStore interface does not return errors. Any error returned by the
// underlying router is therefore surfaced as a panic. This is a short-term
// limitation; the long-term plan is to plumb errors through the interface.
type RouterCommitKVStore struct {
	// routerProvider resolves the owner's current router at call time
	// rather than binding one instance at construction. The owner's
	// router can be replaced while this view is alive (the composite
	// store rebuilds it on a runtime write-mode transition, see
	// composite.SetWriteMode), and views are cached by rootmulti across
	// that boundary — a captured router value would keep routing reads
	// per the pre-transition mode for the rest of the block.
	routerProvider  func() Router
	storeName       string
	versionProvider func() int64
	// iterator builds an iterator over this store's keyspace. Iteration no
	// longer flows through the Router (the backends are stitched together by
	// the owner, e.g. composite.Store); the owner supplies a builder already
	// bound to storeName.
	iterator func(start, end []byte, ascending bool) (db.Iterator, error)
}

func NewRouterCommitKVStore(
	routerProvider func() Router,
	storeName string,
	versionProvider func() int64,
	iterator func(start, end []byte, ascending bool) (db.Iterator, error),
) *RouterCommitKVStore {
	return &RouterCommitKVStore{
		routerProvider:  routerProvider,
		storeName:       storeName,
		versionProvider: versionProvider,
		iterator:        iterator,
	}
}

// Close is illegal during the standard CommitKVStore lifecycle for this type:
// the wrapped Router is owned by the caller and must outlive this view.
func (r *RouterCommitKVStore) Close() error {
	return fmt.Errorf("RouterCommitKVStore.Close: illegal during standard lifecycle")
}

func (r *RouterCommitKVStore) Get(key []byte) []byte {
	value, _, err := r.routerProvider().Read(r.storeName, key)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.Get(store=%q): %w", r.storeName, err))
	}
	return value
}

func (r *RouterCommitKVStore) Has(key []byte) bool {
	_, found, err := r.routerProvider().Read(r.storeName, key)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.Has(store=%q): %w", r.storeName, err))
	}
	return found
}

func (r *RouterCommitKVStore) Set(key []byte, value []byte) {
	r.applyOne(&proto.KVPair{Key: key, Value: value})
}

func (r *RouterCommitKVStore) Remove(key []byte) {
	r.applyOne(&proto.KVPair{Key: key, Delete: true})
}

// applyOne dispatches a single KV change as a one-pair NamedChangeSet through
// the router, panicking on any router error.
func (r *RouterCommitKVStore) applyOne(pair *proto.KVPair) {
	cs := []*proto.NamedChangeSet{{
		Name:      r.storeName,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{pair}},
	}}
	if err := r.routerProvider().ApplyChangeSets(cs, false); err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.ApplyChangeSets(store=%q): %w", r.storeName, err))
	}
}

func (r *RouterCommitKVStore) Iterator(start []byte, end []byte, ascending bool) db.Iterator {
	if r.iterator == nil {
		panic(fmt.Errorf("RouterCommitKVStore.Iterator(store=%q): no iterator builder configured", r.storeName))
	}
	it, err := r.iterator(start, end, ascending)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.Iterator(store=%q): %w", r.storeName, err))
	}
	return it
}

func (r *RouterCommitKVStore) GetProof(key []byte) *ics23.CommitmentProof {
	proof, err := r.routerProvider().GetProof(r.storeName, key)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.GetProof(store=%q): %w", r.storeName, err))
	}
	return proof
}

// RootHash is a placeholder that returns a fresh zeroed 32-byte slice on every
// call. The CommitKVStore contract permits callers to mutate the returned
// slice, so a fresh allocation is required to keep the placeholder safe.
//
// TODO: revisit before shipping to production once the production usage of
// RootHash() across this code path is understood.
func (r *RouterCommitKVStore) RootHash() []byte {
	return make([]byte, rootHashSize)
}

func (r *RouterCommitKVStore) Version() int64 {
	return r.versionProvider()
}
