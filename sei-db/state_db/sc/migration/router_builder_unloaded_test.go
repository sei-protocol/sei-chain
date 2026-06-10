package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

// These tests pin down the contract the migration router exposes to higher
// layers while a memiavl-backed CommitStore is still in its pre-load state
// (i.e. after memiavl.NewCommitStore but before LoadVersion). The mempool
// reactor reaches this code path during state-sync via
// App.CheckTx -> BaseApp.GetConsensusParams -> RouterCommitKVStore.Has, and
// previously panicked on a nil *memiavl.DB dereference.

func newUnloadedMemIAVLForTest(t *testing.T) *memiavl.CommitStore {
	t.Helper()
	cs := memiavl.NewCommitStore(t.TempDir(), memiavl.DefaultConfig())
	require.False(t, cs.IsLoaded(), "precondition: store must not be loaded")
	return cs
}

func TestBuildMemIAVLReader_BeforeLoad_ReportsNotFoundWithoutError(t *testing.T) {
	read := buildMemIAVLReader(newUnloadedMemIAVLForTest(t))

	value, found, err := read("params", []byte("Block"))
	require.NoError(t, err, "reader must not error during the state-sync pre-load window")
	require.False(t, found)
	require.Nil(t, value)
}

func TestBuildMemIAVLWriter_BeforeLoad_RefusesWritesLoudly(t *testing.T) {
	write := buildMemIAVLWriter(newUnloadedMemIAVLForTest(t))

	err := write([]*proto.NamedChangeSet{{
		Name: "params",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}, false)

	require.Error(t, err, "writes before LoadVersion must surface, not corrupt silently")
}

func TestBuildMemIAVLProofBuilder_BeforeLoad_RefusesProof(t *testing.T) {
	proof := buildMemIAVLProofBuilder(newUnloadedMemIAVLForTest(t))

	_, err := proof("params", []byte("k"))
	require.Error(t, err, "proofs require a committed tree and must fail explicitly")
}

// TestModuleRouter_Read_BeforeLoad_DoesNotPanic exercises the exact code path
// that previously panicked at sei-db/state_db/sc/memiavl/db.go:1059 during
// state-sync: RouterCommitKVStore.Has -> ModuleRouter.Read -> reader closure
// -> memiavl.CommitStore.GetChildStoreByName -> memiavl.DB.TreeByName.
func TestModuleRouter_Read_BeforeLoad_DoesNotPanic(t *testing.T) {
	cs := newUnloadedMemIAVLForTest(t)

	route, err := routeToMemIAVL(cs, "params")
	require.NoError(t, err)

	router, err := NewModuleRouter(route)
	require.NoError(t, err)

	value, found, err := router.Read("params", []byte("Block"))
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, value)
}
