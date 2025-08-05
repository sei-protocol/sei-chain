package commitment

import (
	"bytes"
	"fmt"
	"io"

	"cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/store/cachekv"
	"github.com/cosmos/cosmos-sdk/store/listenkv"
	"github.com/cosmos/cosmos-sdk/store/tracekv"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/cosmos/iavl"
	sctypes "github.com/sei-protocol/sei-db/sc/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proto/tendermint/crypto"
)

var (
	_ types.CommitKVStore = (*Store)(nil)
	_ types.Queryable     = (*Store)(nil)
)

// Store Implements types.KVStore and CommitKVStore.
type Store struct {
	tree      sctypes.Tree
	logger    log.Logger
	changeSet iavl.ChangeSet
}

func NewStore(tree sctypes.Tree, logger log.Logger) *Store {
	return &Store{
		tree:   tree,
		logger: logger,
	}
}

func (st *Store) Commit(_ bool) types.CommitID {
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

func (st *Store) GetWorkingHash() ([]byte, error) {
	panic("not implemented")
}

// Implements Store.
func (st *Store) GetStoreType() types.StoreType {
	return types.StoreTypeIAVL
}

func (st *Store) CacheWrap(k types.StoreKey) types.CacheWrap {
	return cachekv.NewStore(st, k, types.DefaultCacheSizeLimit)
}

// CacheWrapWithTrace implements the Store interface.
func (st *Store) CacheWrapWithTrace(k types.StoreKey, w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(st, w, tc), k, types.DefaultCacheSizeLimit)
}

func (st *Store) CacheWrapWithListeners(k types.StoreKey, listeners []types.WriteListener) types.CacheWrap {
	return cachekv.NewStore(listenkv.NewStore(st, k, listeners), k, types.DefaultCacheSizeLimit)
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
func (st *Store) SetInitialVersion(_ int64) {
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
		return sdkerrors.QueryResult(errors.Wrap(sdkerrors.ErrInvalidHeight, "invalid height"))
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
		commitmentProof := st.tree.GetProof(res.Key)
		op := types.NewIavlCommitmentOp(res.Key, commitmentProof)
		res.ProofOps = &crypto.ProofOps{Ops: []crypto.ProofOp{op.ProofOp()}}
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
		return sdkerrors.QueryResult(errors.Wrapf(sdkerrors.ErrUnknownRequest, "unexpected query path: %v", req.Path))
	}

	return res
}

func (st *Store) VersionExists(version int64) bool {
	// one version per SC tree
	return version == st.tree.Version()
}

func (st *Store) DeleteAll(start, end []byte) error {
	iter := st.Iterator(start, end)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		st.Delete(key)
	}
	return nil
}

func (st *Store) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	iter := st.Iterator(start, end)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		res = append(res, string(iter.Key()))
	}
	return
}

func (st *Store) GetChangedPairs(prefix []byte) (res []*iavl.KVPair) {
	// not sure if we can assume pairs are sorted or not, so be conservative
	// here and iterate through everything
	for _, p := range st.changeSet.Pairs {
		if bytes.HasPrefix(p.Key, prefix) {
			res = append(res, p)
		}
	}
	return
}
