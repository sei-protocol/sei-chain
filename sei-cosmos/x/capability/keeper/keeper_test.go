package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// setupKeeper creates a fresh app, context, and capability keeper for a single test.
// It mirrors the original SetupTest hook but is invoked explicitly per test, which makes
// each test self-contained and free of shared state.
func setupKeeper(t *testing.T) (sdk.Context, *keeper.Keeper) {
	t.Helper()

	const checkTx = false
	app := seiapp.Setup(t, checkTx, false, false)

	// Construct a fresh keeper so the test can define custom scoping before Seal is called.
	k := keeper.NewKeeper(app.AppCodec(), app.GetKey(types.StoreKey), app.GetMemKey(types.MemStoreKey))
	ctx := app.BaseApp.NewContext(checkTx, tmproto.Header{Height: 1})

	return ctx, k
}

func TestSeal(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)

	sk := k.ScopeToModule(banktypes.ModuleName)
	require.Panics(t, func() { k.ScopeToModule("  ") }, "whitespace module name must panic")

	// Capture the latest index before creating new capabilities so we can validate that
	// indices are assigned contiguously starting from prevIndex.
	prevIndex := k.GetLatestIndex(ctx)

	caps := make([]*types.Capability, 5)
	for i := range caps {
		cap, err := sk.NewCapability(ctx, fmt.Sprintf("transfer-%d", i))
		require.NoError(t, err)
		require.NotNil(t, cap)
		require.Equal(t, uint64(i)+prevIndex, cap.GetIndex())
		caps[i] = cap
	}

	require.NotPanics(t, func() { k.Seal() })

	// All previously created capabilities remain accessible after Seal.
	for i, cap := range caps {
		got, ok := sk.GetCapability(ctx, fmt.Sprintf("transfer-%d", i))
		require.True(t, ok)
		require.Equal(t, cap, got)
		require.Equal(t, uint64(i)+prevIndex, got.GetIndex())
	}

	require.Panics(t, func() { k.Seal() }, "second Seal must panic")
	require.Panics(t, func() { _ = k.ScopeToModule(stakingtypes.ModuleName) }, "ScopeToModule after Seal must panic")
}

func TestNewCapability(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	// Capability does not exist yet.
	got, ok := sk.GetCapability(ctx, "transfer")
	require.False(t, ok)
	require.Nil(t, got)

	// Create it.
	cap, err := sk.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap)

	// Fetching by name returns the exact same pointer.
	got, ok = sk.GetCapability(ctx, "transfer")
	require.True(t, ok)
	require.Same(t, cap, got, "GetCapability must return the same pointer")

	// Unknown names return nothing.
	got, ok = sk.GetCapability(ctx, "invalid")
	require.False(t, ok)
	require.Nil(t, got)

	// Duplicate creation fails and must not mutate the stored capability.
	cap2, err := sk.NewCapability(ctx, "transfer")
	require.Error(t, err)
	require.Nil(t, cap2)

	got, ok = sk.GetCapability(ctx, "transfer")
	require.True(t, ok)
	require.Same(t, cap, got, "duplicate-creation attempt must not replace stored capability")

	// Whitespace-only names are rejected.
	cap, err = sk.NewCapability(ctx, "   ")
	require.Error(t, err)
	require.Nil(t, cap)
}

func TestAuthenticateCapability(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)

	cap1, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap1)

	// A capability minted by index alone (not via the keeper) must not authenticate
	// in any scope. This is the core security invariant of object-capability tokens.
	forgedCap := types.NewCapability(cap1.Index)
	require.False(t, sk1.AuthenticateCapability(ctx, forgedCap, "transfer"))
	require.False(t, sk2.AuthenticateCapability(ctx, forgedCap, "transfer"))

	cap2, err := sk2.NewCapability(ctx, "bond")
	require.NoError(t, err)
	require.NotNil(t, cap2)

	got, ok := sk1.GetCapability(ctx, "transfer")
	require.True(t, ok)

	require.True(t, sk1.AuthenticateCapability(ctx, cap1, "transfer"))
	require.True(t, sk1.AuthenticateCapability(ctx, got, "transfer"))
	require.False(t, sk1.AuthenticateCapability(ctx, cap1, "invalid"), "wrong name must fail auth")
	require.False(t, sk1.AuthenticateCapability(ctx, cap2, "transfer"), "wrong scope must fail auth")

	require.True(t, sk2.AuthenticateCapability(ctx, cap2, "bond"))
	require.False(t, sk2.AuthenticateCapability(ctx, cap2, "invalid"))
	require.False(t, sk2.AuthenticateCapability(ctx, cap1, "bond"))

	// A released capability must no longer authenticate.
	require.NoError(t, sk2.ReleaseCapability(ctx, cap2))
	require.False(t, sk2.AuthenticateCapability(ctx, cap2, "bond"))

	// Unknown index, whitespace name, and nil capability all fail to authenticate.
	badCap := types.NewCapability(100)
	require.False(t, sk1.AuthenticateCapability(ctx, badCap, "transfer"))
	require.False(t, sk2.AuthenticateCapability(ctx, badCap, "bond"))
	require.False(t, sk1.AuthenticateCapability(ctx, cap1, "  "))
	require.False(t, sk1.AuthenticateCapability(ctx, nil, "transfer"))
}

func TestClaimCapability(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)
	sk3 := k.ScopeToModule("foo")

	cap, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap)

	// The creating module already owns the capability; re-claiming must error.
	require.Error(t, sk1.ClaimCapability(ctx, cap, "transfer"))
	// A different module may claim it under the same name.
	require.NoError(t, sk2.ClaimCapability(ctx, cap, "transfer"))

	// Both owners must see the capability under that name.
	for _, sk := range []keeper.ScopedKeeper{sk1, sk2} {
		got, ok := sk.GetCapability(ctx, "transfer")
		require.True(t, ok)
		require.Equal(t, cap, got)
	}

	require.Error(t, sk3.ClaimCapability(ctx, cap, "  "), "whitespace name must be rejected")
	require.Error(t, sk3.ClaimCapability(ctx, nil, "transfer"), "nil capability must be rejected")
}

func TestGetOwners(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)
	sk3 := k.ScopeToModule("foo")

	cap, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap)

	require.NoError(t, sk2.ClaimCapability(ctx, cap, "transfer"))
	require.NoError(t, sk3.ClaimCapability(ctx, cap, "transfer"))

	// Helper: every scoped keeper in sks must see the same owner set in expectedOrder.
	// Owners are returned in lexicographic order by module name.
	assertOwners := func(t *testing.T, sks []keeper.ScopedKeeper, expectedOrder []string) {
		t.Helper()
		for _, sk := range sks {
			owners, ok := sk.GetOwners(ctx, "transfer")
			require.True(t, ok, "could not retrieve owners")
			require.NotNil(t, owners, "owners is nil")

			mods, gotCap, err := sk.LookupModules(ctx, "transfer")
			require.NoError(t, err, "could not retrieve modules")
			require.NotNil(t, gotCap, "capability is nil")
			require.NotNil(t, mods, "modules is nil")
			require.Equal(t, cap, gotCap, "caps not equal")

			require.Len(t, owners.Owners, len(expectedOrder), "unexpected number of owners")
			for i, o := range owners.Owners {
				require.Equal(t, expectedOrder[i], o.Module, "unexpected module at position %d", i)
				require.Equal(t, expectedOrder[i], mods[i], "unexpected module in lookup at position %d", i)
			}
		}
	}

	assertOwners(t,
		[]keeper.ScopedKeeper{sk1, sk2, sk3},
		[]string{banktypes.ModuleName, "foo", stakingtypes.ModuleName},
	)

	// Once "foo" releases the capability, it disappears from every owner list.
	require.NoError(t, sk3.ReleaseCapability(ctx, cap), "could not release capability")

	assertOwners(t,
		[]keeper.ScopedKeeper{sk1, sk2},
		[]string{banktypes.ModuleName, stakingtypes.ModuleName},
	)

	_, ok := sk1.GetOwners(ctx, "  ")
	require.False(t, ok, "got owners from whitespace capability name")
}

func TestReleaseCapability(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)

	cap1, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap1)
	require.NoError(t, sk2.ClaimCapability(ctx, cap1, "transfer"))

	cap2, err := sk2.NewCapability(ctx, "bond")
	require.NoError(t, err)
	require.NotNil(t, cap2)

	// sk1 cannot release a capability it does not own.
	require.Error(t, sk1.ReleaseCapability(ctx, cap2))

	// After sk2 releases cap1, sk2 no longer sees it — but sk1 still does.
	require.NoError(t, sk2.ReleaseCapability(ctx, cap1))
	got, ok := sk2.GetCapability(ctx, "transfer")
	require.False(t, ok)
	require.Nil(t, got)

	// sk1 releases its own ownership — the capability is fully gone.
	require.NoError(t, sk1.ReleaseCapability(ctx, cap1))
	got, ok = sk1.GetCapability(ctx, "transfer")
	require.False(t, ok)
	require.Nil(t, got)

	require.Error(t, sk1.ReleaseCapability(ctx, nil))
}

func TestCachedCapabilityUsesStableIndexPointer(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)
	ms := ctx.MultiStore()

	// Two independent cached branches both mint a capability at the same in-memory index.
	msCache1 := ms.CacheMultiStore()
	cap1, err := sk.NewCapability(ctx.WithMultiStore(msCache1), "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap1)

	msCache2 := ms.CacheMultiStore()
	cap2, err := sk.NewCapability(ctx.WithMultiStore(msCache2), "stake")
	require.NoError(t, err)
	require.NotNil(t, cap2)
	require.Equal(t, cap1, cap2, "both branches see the same index pointer")

	// Commit branch 1 and confirm the capability survives via the root context.
	msCache1.Write()

	got, ok := sk.GetCapability(ctx, "transfer")
	require.True(t, ok)
	require.Equal(t, cap1, got)
	require.True(t, sk.AuthenticateCapability(ctx, got, "transfer"))
}

func TestCachedReleaseKeepsCapabilityIndexAvailable(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	cap, err := sk.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap)

	// Release the capability on a cached branch — do NOT commit it.
	msCache := ctx.MultiStore().CacheMultiStore()
	require.NoError(t, sk.ReleaseCapability(ctx.WithMultiStore(msCache), cap))

	// Root context must still see the capability since the release was never written.
	require.NotPanics(t, func() {
		got, ok := sk.GetCapability(ctx, "transfer")
		require.True(t, ok)
		require.Equal(t, cap, got)
	})
	require.True(t, sk.AuthenticateCapability(ctx, cap, "transfer"))
}

func TestRevertCapability(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	msCache := ctx.MultiStore().CacheMultiStore()
	cacheCtx := ctx.WithMultiStore(msCache)

	const capName = "revert"

	// Create the capability on the cached context only.
	cap, err := sk.NewCapability(cacheCtx, capName)
	require.NoError(t, err, "could not create capability")

	// Cached context sees it.
	gotCache, ok := sk.GetCapability(cacheCtx, capName)
	require.True(t, ok, "could not retrieve capability from cached context")
	require.Equal(t, cap, gotCache, "did not get correct capability from cached context")

	// Root context does NOT see it yet — the write is still pending.
	got, ok := sk.GetCapability(ctx, capName)
	require.False(t, ok, "retrieved capability from root context before write")
	require.Nil(t, got, "capability not nil in root store")

	// Commit and re-check visibility from the root context.
	msCache.Write()

	got, ok = sk.GetCapability(ctx, capName)
	require.True(t, ok, "could not retrieve capability from root context after write")
	require.Equal(t, cap, got, "did not get correct capability from root context after write")
}

// TestScopeToModule_DuplicateModulePanics covers the third panic branch in
// ScopeToModule (duplicate registration), which the original suite missed.
func TestScopeToModule_DuplicateModulePanics(t *testing.T) {
	t.Parallel()
	_, k := setupKeeper(t)
	_ = k.ScopeToModule(banktypes.ModuleName)

	require.Panics(t, func() { k.ScopeToModule(banktypes.ModuleName) },
		"creating two scoped keepers for the same module name must panic")
}

// TestSeal_AllowsExistingScopesToMintCapabilities verifies that Seal only blocks
// new ScopeToModule calls — existing ScopedKeepers must remain fully operational.
// Without this, a regression that wires Seal into ScopedKeeper would slip through.
func TestSeal_AllowsExistingScopesToMintCapabilities(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	k.Seal()

	cap, err := sk.NewCapability(ctx, "post-seal")
	require.NoError(t, err, "existing scoped keeper must still mint after Seal")
	require.NotNil(t, cap)

	require.True(t, sk.AuthenticateCapability(ctx, cap, "post-seal"))
}

// TestNewCapability_NameIsolatedAcrossModules exercises the keeper's core
// namespacing invariant: the same capability name in two different modules
// produces two distinct capabilities, each visible only to its owning scope.
func TestNewCapability_NameIsolatedAcrossModules(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)

	cap1, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap1)

	cap2, err := sk2.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.NotNil(t, cap2)

	// Distinct capabilities → distinct indices.
	require.NotEqual(t, cap1.GetIndex(), cap2.GetIndex(),
		"per-module name isolation must produce distinct global indices")

	// Each scope authenticates only its own capability under that name.
	require.True(t, sk1.AuthenticateCapability(ctx, cap1, "transfer"))
	require.False(t, sk1.AuthenticateCapability(ctx, cap2, "transfer"))
	require.True(t, sk2.AuthenticateCapability(ctx, cap2, "transfer"))
	require.False(t, sk2.AuthenticateCapability(ctx, cap1, "transfer"))
}

// TestGetCapability_ScopeIsolation verifies a module cannot retrieve another
// module's capability by name even when the name exists in the system.
func TestGetCapability_ScopeIsolation(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk1 := k.ScopeToModule(banktypes.ModuleName)
	sk2 := k.ScopeToModule(stakingtypes.ModuleName)

	_, err := sk1.NewCapability(ctx, "transfer")
	require.NoError(t, err)

	// sk2 never claimed "transfer" — it must not be retrievable from that scope.
	got, ok := sk2.GetCapability(ctx, "transfer")
	require.False(t, ok, "GetCapability must respect scope ownership")
	require.Nil(t, got)
}

// TestInitializeIndex covers both panic branches of InitializeIndex (index == 0
// and index already set). The success branch is implicitly exercised by app
// genesis bootstrapping in setupKeeper.
func TestInitializeIndex(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)

	// index == 0 always panics.
	require.Panics(t, func() { _ = k.InitializeIndex(ctx, 0) },
		"InitializeIndex(0) must panic")

	// Bump the global index past zero by minting a capability.
	sk := k.ScopeToModule(banktypes.ModuleName)
	_, err := sk.NewCapability(ctx, "transfer")
	require.NoError(t, err)
	require.Greater(t, k.GetLatestIndex(ctx), uint64(0))

	// With the index now > 0, InitializeIndex must panic regardless of the value.
	require.Panics(t, func() { _ = k.InitializeIndex(ctx, 1) },
		"InitializeIndex must panic when the global index is already set")
}

// TestIsInitialized verifies the memstore-initialization flag is set after app
// setup (genesis runs InitMemStore as part of bringup) and that calling
// InitMemStore again is a safe no-op.
func TestIsInitialized(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)

	require.True(t, k.IsInitialized(ctx),
		"memstore should be initialized after app setup")

	// Idempotency: repeated calls must not panic and must leave the flag set.
	require.NotPanics(t, func() { k.InitMemStore(ctx) })
	require.True(t, k.IsInitialized(ctx))
}

// TestGetCapabilityName covers the method directly across its three branches:
// nil input, owning scope, and a non-owning scope.
func TestGetCapabilityName(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)
	skOther := k.ScopeToModule(stakingtypes.ModuleName)

	// nil capability → empty name (no panic).
	require.Empty(t, sk.GetCapabilityName(ctx, nil))

	cap, err := sk.NewCapability(ctx, "transfer")
	require.NoError(t, err)

	// Owning scope returns the registered name.
	require.Equal(t, "transfer", sk.GetCapabilityName(ctx, cap))

	// A different scope that doesn't own this capability must return empty —
	// otherwise the OCAP isolation property would leak names across scopes.
	require.Empty(t, skOther.GetCapabilityName(ctx, cap),
		"non-owning scope must not learn the capability name")
}

// TestLookupModules_ErrorPaths covers the two error branches in LookupModules
// that the existing TestGetOwners only happy-paths through.
func TestLookupModules_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	// Whitespace name is rejected before any store lookup.
	mods, cap, err := sk.LookupModules(ctx, "  ")
	require.Error(t, err)
	require.Nil(t, mods)
	require.Nil(t, cap)

	// Non-existent capability returns an error and nil results.
	mods, cap, err = sk.LookupModules(ctx, "does-not-exist")
	require.Error(t, err)
	require.Nil(t, mods)
	require.Nil(t, cap)
}

// TestKeeperGetSetOwners exercises the Keeper-level (not ScopedKeeper-level)
// owner read/write API, which the original suite never touched. This is the
// surface used by genesis import/export.
func TestKeeperGetSetOwners(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)

	// Reading a never-set index returns the zero owners and false.
	owners, ok := k.GetOwners(ctx, 12345)
	require.False(t, ok)
	require.Empty(t, owners.Owners)

	// Write a synthetic owners record at a free index and round-trip it.
	initial := types.CapabilityOwners{
		Owners: []types.Owner{
			{Module: banktypes.ModuleName, Name: "transfer"},
			{Module: stakingtypes.ModuleName, Name: "transfer"},
		},
	}
	k.SetOwners(ctx, 42, initial)

	got, ok := k.GetOwners(ctx, 42)
	require.True(t, ok)
	require.Equal(t, initial.Owners, got.Owners)

	// As a cross-check, NewCapability should also populate the persistent
	// owners store at the capability's own index.
	sk := k.ScopeToModule(banktypes.ModuleName)
	cap, err := sk.NewCapability(ctx, "from-new")
	require.NoError(t, err)

	persisted, ok := k.GetOwners(ctx, cap.GetIndex())
	require.True(t, ok, "NewCapability must populate persistent owners store")
	require.Len(t, persisted.Owners, 1)
	require.Equal(t, banktypes.ModuleName, persisted.Owners[0].Module)
	require.Equal(t, "from-new", persisted.Owners[0].Name)
}

func TestReleaseCapability_FinalReleaseClearsPersistentEntry(t *testing.T) {
	t.Parallel()
	ctx, k := setupKeeper(t)
	sk := k.ScopeToModule(banktypes.ModuleName)

	cap, err := sk.NewCapability(ctx, "transfer")
	require.NoError(t, err)

	// Persistent entry exists immediately after creation.
	_, ok := k.GetOwners(ctx, cap.GetIndex())
	require.True(t, ok, "persistent owners entry must exist after creation")

	// Release the only owner.
	require.NoError(t, sk.ReleaseCapability(ctx, cap))

	// Persistent entry must be gone (not just empty).
	_, ok = k.GetOwners(ctx, cap.GetIndex())
	require.False(t, ok, "persistent owners entry must be removed after final release")

	// And the in-memory mapping is also clear.
	got, found := sk.GetCapability(ctx, "transfer")
	require.False(t, found)
	require.Nil(t, got)
}
