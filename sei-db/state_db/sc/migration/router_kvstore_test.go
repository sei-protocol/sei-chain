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

// staticRouter wraps a fixed Router in the provider form the constructor
// expects. Tests that exercise provider liveness build their own closure.
func staticRouter(r Router) func() Router {
	return func() Router { return r }
}

// newRouterCommitKVStoreForTest returns a RouterCommitKVStore wrapping a fresh
// TestInMemoryRouter with a constant version. Tests that need a different
// inner router or a non-constant versionProvider construct the store
// directly.
func newRouterCommitKVStoreForTest(t *testing.T, version int64) (*RouterCommitKVStore, *TestInMemoryRouter) {
	t.Helper()
	inner := NewTestInMemoryRouter()
	store := NewRouterCommitKVStore(staticRouter(inner), testRouterStoreName, func() int64 { return version }, nil)
	return store, inner
}

// TestRouterCommitKVStore_RouterProviderResolvesPerCall pins the live-binding
// contract: the provider is consulted on every operation, so an owner that
// swaps its router (e.g. composite.SetWriteMode on a runtime write-mode
// transition) is immediately visible through views created before the swap.
func TestRouterCommitKVStore_RouterProviderResolvesPerCall(t *testing.T) {
	first := NewTestInMemoryRouter()
	second := NewTestInMemoryRouter()
	current := Router(first)
	store := NewRouterCommitKVStore(
		func() Router { return current },
		testRouterStoreName,
		func() int64 { return 0 },
		nil,
	)

	store.Set([]byte("k"), []byte("via-first"))
	require.Equal(t, []byte("via-first"), store.Get([]byte("k")))

	current = second
	require.Nil(t, store.Get([]byte("k")),
		"after the swap, reads must hit the new router (which has no data)")
	store.Set([]byte("k"), []byte("via-second"))
	require.Equal(t, []byte("via-second"), store.Get([]byte("k")))

	val, found, err := first.Read(testRouterStoreName, []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("via-first"), val,
		"the pre-swap write must have landed on the first router only")
}

func TestRouterCommitKVStore_GetReturnsValueWrittenViaRouter(t *testing.T) {
	store, inner := newRouterCommitKVStoreForTest(t, 0)
	require.NoError(t, inner.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: testRouterStoreName,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}, true))

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
	bankStore := NewRouterCommitKVStore(staticRouter(inner), "bank", func() int64 { return 0 }, nil)
	evmStore := NewRouterCommitKVStore(staticRouter(inner), "evm", func() int64 { return 0 }, nil)

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
	store := NewRouterCommitKVStore(staticRouter(NewTestInMemoryRouter()), testRouterStoreName, func() int64 { return current }, nil)

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

func TestRouterCommitKVStore_IteratorPanicsOnBuilderError(t *testing.T) {
	// An iterator builder that returns an error must be surfaced as a panic.
	store := NewRouterCommitKVStore(
		staticRouter(NewTestInMemoryRouter()),
		testRouterStoreName,
		func() int64 { return 0 },
		func([]byte, []byte, bool) (dbm.Iterator, error) {
			return nil, errors.New("boom")
		},
	)
	require.Panics(t, func() { _ = store.Iterator(nil, nil, true) })
}

func TestRouterCommitKVStore_IteratorPanicsWhenNoBuilderConfigured(t *testing.T) {
	// With no iterator builder configured, calling Iterator must panic rather
	// than nil-dereference.
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
// the methods that TestInMemoryRouter implements without errors. GetProof
// always returns a not-implemented error and is not used by these tests;
// TestInMemoryRouter already covers its panic path.
type failingRouter struct {
	readErr  error
	writeErr error
}

var _ Router = (*failingRouter)(nil)

func (f *failingRouter) Read(string, []byte) ([]byte, bool, error) {
	return nil, false, f.readErr
}

func (f *failingRouter) ApplyChangeSets([]*proto.NamedChangeSet, bool) error {
	return f.writeErr
}

func (f *failingRouter) GetProof(string, []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("failingRouter.GetProof: not used by these tests")
}

func (f *failingRouter) SetMigrationBatchSize(int) {}

func TestRouterCommitKVStore_GetPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		staticRouter(&failingRouter{readErr: errors.New("boom")}),
		testRouterStoreName,
		func() int64 { return 0 },
		nil,
	)
	require.PanicsWithError(t, `RouterCommitKVStore.Get(store="bank"): boom`, func() {
		_ = store.Get([]byte("k"))
	})
}

func TestRouterCommitKVStore_HasPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		staticRouter(&failingRouter{readErr: errors.New("boom")}),
		testRouterStoreName,
		func() int64 { return 0 },
		nil,
	)
	require.PanicsWithError(t, `RouterCommitKVStore.Has(store="bank"): boom`, func() {
		_ = store.Has([]byte("k"))
	})
}

func TestRouterCommitKVStore_SetPanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		staticRouter(&failingRouter{writeErr: errors.New("boom")}),
		testRouterStoreName,
		func() int64 { return 0 },
		nil,
	)
	require.PanicsWithError(t, `RouterCommitKVStore.ApplyChangeSets(store="bank"): boom`, func() {
		store.Set([]byte("k"), []byte("v"))
	})
}

func TestRouterCommitKVStore_RemovePanicsOnRouterError(t *testing.T) {
	store := NewRouterCommitKVStore(
		staticRouter(&failingRouter{writeErr: errors.New("boom")}),
		testRouterStoreName,
		func() int64 { return 0 },
		nil,
	)
	require.PanicsWithError(t, `RouterCommitKVStore.ApplyChangeSets(store="bank"): boom`, func() {
		store.Remove([]byte("k"))
	})
}
