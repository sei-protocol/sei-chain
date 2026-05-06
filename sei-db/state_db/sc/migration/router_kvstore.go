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
	router          Router
	storeName       string
	versionProvider func() int64
}

func NewRouterCommitKVStore(
	router Router,
	storeName string,
	versionProvider func() int64,
) *RouterCommitKVStore {
	return &RouterCommitKVStore{
		router:          router,
		storeName:       storeName,
		versionProvider: versionProvider,
	}
}

// Close is illegal during the standard CommitKVStore lifecycle for this type:
// the wrapped Router is owned by the caller and must outlive this view.
func (r *RouterCommitKVStore) Close() error {
	panic("RouterCommitKVStore.Close: illegal during standard lifecycle")
}

func (r *RouterCommitKVStore) Get(key []byte) []byte {
	value, _, err := r.router.Read(r.storeName, key)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.Get(store=%q): %w", r.storeName, err))
	}
	return value
}

func (r *RouterCommitKVStore) Has(key []byte) bool {
	_, found, err := r.router.Read(r.storeName, key)
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
	if err := r.router.ApplyChangeSets(cs); err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.ApplyChangeSets(store=%q): %w", r.storeName, err))
	}
}

func (r *RouterCommitKVStore) Iterator(start []byte, end []byte, ascending bool) db.Iterator {
	it, err := r.router.Iterator(r.storeName, start, end, ascending)
	if err != nil {
		panic(fmt.Errorf("RouterCommitKVStore.Iterator(store=%q): %w", r.storeName, err))
	}
	return it
}

func (r *RouterCommitKVStore) GetProof(key []byte) *ics23.CommitmentProof {
	proof, err := r.router.GetProof(r.storeName, key)
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
