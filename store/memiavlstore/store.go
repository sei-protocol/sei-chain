package memiavlstore

import (
	"fmt"
	"io"

	"cosmossdk.io/errors"
	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/memiavl"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmcrypto "github.com/tendermint/tendermint/proto/tendermint/crypto"

	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

var (
	_ types.KVStore       = (*Store)(nil)
	_ types.CommitStore   = (*Store)(nil)
	_ types.CommitKVStore = (*Store)(nil)
	_ types.Queryable     = (*Store)(nil)
)

// Store Implements types.KVStore and CommitKVStore.
type Store struct {
	tree   *memiavl.Tree
	logger log.Logger

	changeSet iavl.ChangeSet
}

func New(tree *memiavl.Tree, logger log.Logger) *Store {
	return &Store{tree: tree, logger: logger}
}

func (st *Store) Commit() types.CommitID {
	panic("memiavl store is not supposed to be committed alone")
}

func (st *Store) LastCommitID() types.CommitID {
	hash := st.tree.RootHash()
	return types.CommitID{
		Version: st.tree.Version(),
		Hash:    hash,
	}
}

// SetPruning panics as pruning options should be provided at initialization
// since IAVl accepts pruning options directly.
func (st *Store) SetPruning(_ types.PruningOptions) {
	panic("cannot set pruning options on an initialized IAVL store")
}

// SetPruning panics as pruning options should be provided at initialization
// since IAVl accepts pruning options directly.
func (st *Store) GetPruning() types.PruningOptions {
	panic("cannot get pruning options on an initialized IAVL store")
}

// Implements Store.
func (st *Store) GetStoreType() types.StoreType {
	return types.StoreTypeIAVL
}

func (st *Store) CacheWrap() types.CacheWrap {
	return cachekv.NewStore(st)
}

// CacheWrapWithTrace implements the Store interface.
func (st *Store) CacheWrapWithTrace(w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(st, w, tc))
}

// Implements types.KVStore.
//
// we assume Set is only called in `Commit`, so the written state is only visible after commit.
func (st *Store) Set(key, value []byte) {
	st.changeSet.Pairs = append(st.changeSet.Pairs, &iavl.KVPair{
		Key: key, Value: value,
	})
}

// Implements types.KVStore.
func (st *Store) Get(key []byte) []byte {
	return st.tree.Get(key)
}

// Implements types.KVStore.
func (st *Store) Has(key []byte) bool {
	return st.tree.Has(key)
}

// Implements types.KVStore.
//
// we assume Delete is only called in `Commit`, so the written state is only visible after commit.
func (st *Store) Delete(key []byte) {
	st.changeSet.Pairs = append(st.changeSet.Pairs, &iavl.KVPair{
		Key: key, Delete: true,
	})
}

func (st *Store) Iterator(start, end []byte) types.Iterator {
	return st.tree.Iterator(start, end, true)
}

func (st *Store) ReverseIterator(start, end []byte) types.Iterator {
	return st.tree.Iterator(start, end, false)
}

// SetInitialVersion sets the initial version of the IAVL tree. It is used when
// starting a new chain at an arbitrary height.
// implements interface StoreWithInitialVersion
func (st *Store) SetInitialVersion(version int64) {
	panic("memiavl store's SetInitialVersion is not supposed to be called directly")
}

// PopChangeSet returns the change set and clear it
func (st *Store) PopChangeSet() iavl.ChangeSet {
	cs := st.changeSet
	st.changeSet = iavl.ChangeSet{}
	return cs
}

func (st *Store) Query(req abci.RequestQuery) (res abci.ResponseQuery) {
	if req.Height > 0 && req.Height != st.tree.Version() {
		return sdkerrors.QueryResult(errors.Wrap(sdkerrors.ErrInvalidHeight, "invalid height"), false)
	}

	res.Height = st.tree.Version()

	switch req.Path {
	case "/key": // get by key
		res.Key = req.Data // data holds the key bytes
		res.Value = st.tree.Get(res.Key)

		if !req.Prove {
			break
		}

		// get proof from tree and convert to merkle.Proof before adding to result
		res.ProofOps = getProofFromTree(st.tree, req.Data, res.Value != nil)
	case "/subspace":
		pairs := kv.Pairs{
			Pairs: make([]kv.Pair, 0),
		}

		subspace := req.Data
		res.Key = subspace

		iterator := types.KVStorePrefixIterator(st, subspace)
		for ; iterator.Valid(); iterator.Next() {
			pairs.Pairs = append(pairs.Pairs, kv.Pair{Key: iterator.Key(), Value: iterator.Value()})
		}
		iterator.Close()

		bz, err := pairs.Marshal()
		if err != nil {
			panic(fmt.Errorf("failed to marshal KV pairs: %w", err))
		}

		res.Value = bz
	default:
		return sdkerrors.QueryResult(errors.Wrapf(sdkerrors.ErrUnknownRequest, "unexpected query path: %v", req.Path), false)
	}

	return res
}

// Takes a MutableTree, a key, and a flag for creating existence or absence proof and returns the
// appropriate merkle.Proof. Since this must be called after querying for the value, this function should never error
// Thus, it will panic on error rather than returning it
func getProofFromTree(tree *memiavl.Tree, key []byte, exists bool) *tmcrypto.ProofOps {
	var (
		commitmentProof *ics23.CommitmentProof
		err             error
	)

	if exists {
		// value was found
		commitmentProof, err = tree.GetMembershipProof(key)
		if err != nil {
			// sanity check: If value was found, membership proof must be creatable
			panic(fmt.Sprintf("unexpected value for empty proof: %s", err.Error()))
		}
	} else {
		// value wasn't found
		commitmentProof, err = tree.GetNonMembershipProof(key)
		if err != nil {
			// sanity check: If value wasn't found, nonmembership proof must be creatable
			panic(fmt.Sprintf("unexpected error for nonexistence proof: %s", err.Error()))
		}
	}

	op := types.NewIavlCommitmentOp(key, commitmentProof)
	return &tmcrypto.ProofOps{Ops: []tmcrypto.ProofOp{op.ProofOp()}}
}
