package migration

import (
	"errors"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const testRouterStoreName = "bank"

// newRouterCommitKVStoreForTest returns a RouterCommitKVStore wrapping a fresh
// TestInMemoryRouter with a constant version. Tests that need a different
// inner router or a non-constant versionProvider construct the store
// directly.
func newRouterCommitKVStoreForTest(t *testing.T, version int64) (*RouterCommitKVStore, *TestInMemoryRouter) {
	t.Helper()
	inner := NewTestInMemoryRouter()
	store := NewRouterCommitKVStore(inner, testRouterStoreName, func() int64 { return version })
	return store, inner
}

func TestRouterCommitKVStore_GetReturnsValueWrittenViaRouter(t *testing.T) {
	store, inner := newRouterCommitKVStoreForTest(t, 0)
	require.NoError(t, inner.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: testRouterStoreName,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}))

	require.Equal(t, []byte("v"), store.Get([]byte("k")))
	require.True(t, store.Has([]byte("k")))
}

func TestRouterCommitKVStore_GetMissingKeyReturnsNil(t *testing.T) {
	store, _ := newRouterCommitKVStoreForTest(t, 0)
	require.Nil(t, store.Get([]byte("missing")))
	require.False(t, store.Has([]byte("missing")))
}

func TestRouterCommitKVStore_SetWritesViaRouter(t *testing.T) {
	store, inner := newRouterCommitKVStoreForTest(t, 0)
	store.Set([]byte("k"), []byte("v"))

	val, found, err := inner.Read(testRouterStoreName, []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
}

func TestRouterCommitKVStore_RemoveDeletesViaRouter(t *testing.T) {
	store, inner := newRouterCommitKVStoreForTest(t, 0)
	store.Set([]byte("k"), []byte("v"))
	store.Remove([]byte("k"))

	val, found, err := inner.Read(testRouterStoreName, []byte("k"))
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

// TestRouterCommitKVStore_BindsToSingleStoreName confirms that two wrappers
// pointed at the same router but bound to different module names see only
// their own data, and writes land under the configured store name in the
// underlying router.
func TestRouterCommitKVStore_BindsToSingleStoreName(t *testing.T) {
	inner := NewTestInMemoryRouter()
	bankStore := NewRouterCommitKVStore(inner, "bank", func() int64 { return 0 })
	evmStore := NewRouterCommitKVStore(inner, "evm", func() int64 { return 0 })

	bankStore.Set([]byte("k"), []byte("from-bank"))
	evmStore.Set([]byte("k"), []byte("from-evm"))

	require.Equal(t, []byte("from-bank"), bankStore.Get([]byte("k")))
	require.Equal(t, []byte("from-evm"), evmStore.Get([]byte("k")))

	val, found, err := inner.Read("bank", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("from-bank"), val)

	val, found, err = inner.Read("evm", []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("from-evm"), val)
}

// TestRouterCommitKVStore_VersionInvokesProviderEachCall confirms that the
// versionProvider lambda is consulted on every Version() call rather than
// captured once at construction time, so callers can swap the value at
// runtime.
func TestRouterCommitKVStore_VersionInvokesProviderEachCall(t *testing.T) {
	current := int64(7)
	store := NewRouterCommitKVStore(NewTestInMemoryRouter(), testRouterStoreName, func() int64 { return current })

	require.Equal(t, int64(7), store.Version())
	current = 42
	require.Equal(t, int64(42), store.Version())
}

// TestRouterCommitKVStore_RootHashIs32ZeroBytes locks in the placeholder
// contract: 32 bytes, all zero, freshly allocated on every call so that a
// caller mutating the returned slice cannot corrupt subsequent reads.
func TestRouterCommitKVStore_RootHashIs32ZeroBytes(t *testing.T) {
	store, _ := newRouterCommitKVStoreForTest(t, 0)
	hash := store.RootHash()
	require.Len(t, hash, 32)
	require.Equal(t, make([]byte, 32), hash)

	hash[0] = 0xFF
	hash2 := store.RootHash()
	require.Len(t, hash2, 32)
	require.Equal(t, make([]byte, 32), hash2)
}

// TestRouterCommitKVStore_CloseReturnsError locks in that Close is illegal
// during the standard lifecycle: the wrapped Router is owned by the caller
// and must outlive this view, so Close surfaces a non-nil error rather than
// performing any teardown.
func TestRouterCommitKVStore_CloseReturnsError(t *testing.T) {
	store, _ := newRouterCommitKVStoreForTest(t, 0)
	err := store.Close()
	require.EqualError(t, err, "RouterCommitKVStore.Close: illegal during standard lifecycle")
}

func TestRouterCommitKVStore_IteratorPanicsOnRouterError(t *testing.T) {
	// TestInMemoryRouter.Iterator always returns an error; the wrapper must
	// surface that as a panic.
	store, _ := newRouterCommitKVStoreForTest(t, 0)
	require.Panics(t, func() { _ = store.Iterator(nil, nil, true) })
}

func TestRouterCommitKVStore_GetProofPanicsOnRouterError(t *testing.T) {
	// TestInMemoryRouter.GetProof always returns an error; the wrapper must
	// surface that as a panic.
	store, _ := newRouterCommitKVStoreForTest(t, 0)
	require.Panics(t, func() { _ = store.GetProof([]byte("k")) })
}

// failingRouter is a Router whose Read and ApplyChangeSets return injected
// sentinel errors. It exists so we can exercise the panic-on-error path for
// the methods that TestInMemoryRouter implements without errors. Iterator and
// GetProof always return a not-implemented error and are not used by these
// tests; TestInMemoryRouter already covers their panic paths.
type failingRouter struct {
	readErr  error
	writeErr error
}

var _ Router = (*failingRouter)(nil)

func (f *failingRouter) Read(string, []byte) ([]byte, bool, error) {
	return nil, false, f.readErr
}

func (f *failingRouter) ApplyChangeSets([]*proto.NamedChangeSet) error {
	return f.writeErr
}

func (f *failingRouter) Iterator(string, []byte, []byte, bool) (dbm.Iterator, error) {
	return nil, errors.New("failingRouter.Iterator: not used by these tests")
}

func (f *failingRouter) GetProof(string, []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("failingRouter.GetProof: not used by these tests")
}

func TestRouterCommitKVStore_GetPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		&failingRouter{readErr: errors.New("boom")},
		testRouterStoreName,
		func() int64 { return 0 },
	)
	require.PanicsWithError(t, `RouterCommitKVStore.Get(store="bank"): boom`, func() {
		_ = store.Get([]byte("k"))
	})
}

func TestRouterCommitKVStore_HasPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		&failingRouter{readErr: errors.New("boom")},
		testRouterStoreName,
		func() int64 { return 0 },
	)
	require.PanicsWithError(t, `RouterCommitKVStore.Has(store="bank"): boom`, func() {
		_ = store.Has([]byte("k"))
	})
}

func TestRouterCommitKVStore_SetPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		&failingRouter{writeErr: errors.New("boom")},
		testRouterStoreName,
		func() int64 { return 0 },
	)
	require.PanicsWithError(t, `RouterCommitKVStore.ApplyChangeSets(store="bank"): boom`, func() {
		store.Set([]byte("k"), []byte("v"))
	})
}

func TestRouterCommitKVStore_RemovePanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		&failingRouter{writeErr: errors.New("boom")},
		testRouterStoreName,
		func() int64 { return 0 },
	)
	require.PanicsWithError(t, `RouterCommitKVStore.ApplyChangeSets(store="bank"): boom`, func() {
		store.Remove([]byte("k"))
	})
}
