package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TestFlatKVNeededAtHeight covers the classification table. Every row is a pure function of the cached
// earliest-history record, the configured mode and the height — the function performs no I/O, which is what
// lets historical queries call it per read.
func TestFlatKVNeededAtHeight(t *testing.T) {
	for _, tc := range []struct {
		name string
		// present is whether the constructor materialized a flatkv backend at all.
		present bool
		// earliest is flatkv's earliest-history record, 0 when unseeded.
		earliest int64
		mode     types.WriteMode
		height   int64
		want     bool
	}{
		// Under types.Auto the constructor only builds flatkv when its directory already exists, so
		// absence is itself the answer and must not be treated as "unknown".
		{name: "not materialized", present: false, earliest: 0, mode: types.Auto, height: 5, want: false},

		// A fixed configured mode cannot re-derive an effective memiavl-only layout, so it answers from
		// config alone — even at a height the record would call pre-era.
		{name: "fixed mode ignores era", present: true, earliest: 10, mode: types.EVMMigrated,
			height: 5, want: true},

		// The latest height is in-era by definition whenever flatkv exists at all.
		{name: "latest", present: true, earliest: 10, mode: types.Auto, height: 0, want: true},

		// The pre-era case this whole mechanism exists for, and its boundary: the record is the first
		// in-era height, so height == earliest still needs flatkv.
		{name: "below earliest", present: true, earliest: 10, mode: types.Auto, height: 9, want: false},
		{name: "at earliest", present: true, earliest: 10, mode: types.Auto, height: 10, want: true},
		{name: "above earliest", present: true, earliest: 10, mode: types.Auto, height: 11, want: true},

		// An unseeded record means history begins at genesis or seeding never ran. Both resolve toward
		// "yes": answering "no" would serve the height from a memiavl whose migrated keys are deleted,
		// fabricating a nonexistence answer, whereas "yes" fails loudly on the flatkv open.
		{name: "unseeded record", present: true, earliest: 0, mode: types.Auto, height: 5, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, FlatKVNeededAtHeight(tc.present, tc.earliest, tc.mode, tc.height))
		})
	}
}

// TestNewCompositeCommitStore_UnreadableFlatKVMetadataFails pins the fail-loud direction at the moment the
// era record is read. A flatkv directory that exists but whose metadata cannot be read is corrupt or
// mid-materialization from a crashed transition, and those are not distinguishable here. Defaulting to an
// unseeded record would classify every height as in-era, which is safe, but defaulting the other way — or
// treating the directory as absent — would serve heights from a memiavl whose migrated keys were deleted. So
// the constructor refuses to produce a store at all.
func TestNewCompositeCommitStore_UnreadableFlatKVMetadataFails(t *testing.T) {
	dir := t.TempDir()

	// A regular file where the working metadata DB belongs: the directory exists, so the constructor
	// builds flatkv, but the point read cannot open it.
	metaDir := filepath.Join(utils.GetFlatKVPath(dir), "working", "metadata")
	require.NoError(t, os.MkdirAll(filepath.Dir(metaDir), 0o750))
	require.NoError(t, os.WriteFile(metaDir, []byte("not a pebble db"), 0o600))

	_, err := NewCompositeCommitStore(t.Context(), dir, autoExportConfig())
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to read FlatKV earliest version")
}

// TestComposite_Auto_EraRecordTracksLiveTransition guards the one moment the constructor's on-disk read goes
// stale: a MigrateEVM transition that materializes and seeds flatkv mid-run. A store that kept the value it
// read at construction (0, because the directory did not exist yet) would classify every pre-transition
// height as in-era and send historical reads into a flatkv that has no data there.
func TestComposite_Auto_EraRecordTracksLiveTransition(t *testing.T) {
	dir := t.TempDir()
	cfg := autoExportConfig()

	cs := openAutoStoreWithConfig(t, dir, cfg, 100)
	defer func() { _ = cs.Close() }()

	require.Nil(t, cs.flatKV, "fixture precondition: flatkv must not be materialized yet")
	require.Zero(t, cs.flatKVEarliestVersion)

	for i := 1; i <= 5; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte{0x10 + byte(i)}},
			}}},
		}))
		_, err := cs.Commit()
		require.NoError(t, err)
	}

	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.NotNil(t, cs.flatKV, "the transition must have materialized flatkv")
	require.Equal(t, cs.flatKV.EarliestVersion(), cs.flatKVEarliestVersion,
		"the cached record must follow the seeding the transition performed")
	require.Equal(t, int64(5), cs.flatKVEarliestVersion)

	// The classification must now split history at the transition height.
	require.False(t, FlatKVNeededAtHeight(true, cs.flatKVEarliestVersion, cfg.WriteMode, 3))
	require.True(t, FlatKVNeededAtHeight(true, cs.flatKVEarliestVersion, cfg.WriteMode, 5))
}

// TestComposite_Auto_ReadOnlyPreEraHeightOnNeverLoadedStore is the regression guard for the gap this function
// closes. It drives the same pre-era scenario as TestComposite_Auto_ReadOnlyPreFlatKVEraHeight, but serves the
// historical read from a freshly constructed store that has never been loaded — the shape rootmulti uses for
// `seid export --height N`, where the era decision cannot come from flatkv's in-memory bookkeeping because
// nothing has populated it.
func TestComposite_Auto_ReadOnlyPreEraHeightOnNeverLoadedStore(t *testing.T) {
	dir := t.TempDir()
	cfg := autoExportConfig()

	valAt := func(i int) []byte { return []byte{0x10 + byte(i)} }

	cs := openAutoStoreWithConfig(t, dir, cfg, 100)

	// Heights 1..5 in the memiavl-only era, each with a distinct value so the as-of-height read is checkable.
	for i := 1; i <= 5; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: valAt(i)},
			}}},
		}))
		_, err := cs.Commit()
		require.NoError(t, err)
	}

	// Transition at height 5, then drive blocks so flatkv accumulates committed history.
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	for i := 0; i < 3; i++ {
		require.NoError(t, cs.ApplyChangeSets(nil))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(5), cs.flatKV.EarliestVersion(),
		"fixture precondition: flatkv history must begin at the transition height")
	require.NoError(t, cs.Close())

	// Reopen without loading. Initialize mirrors what rootmulti does before vending a historical view; the
	// deliberate omission is LoadLatest, which is what would otherwise populate flatkv's bookkeeping.
	fresh, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	defer func() { _ = fresh.Close() }()
	require.NoError(t, fresh.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	// Pre-era height: must be served memiavl-only, with the value as of that height.
	preEra, err := fresh.LoadVersionReadOnly(3)
	require.NoError(t, err, "pre-flatkv-era heights must be queryable from a never-loaded store")
	preEraStore, ok := preEra.(*CompositeCommitStore)
	require.True(t, ok)
	require.Nil(t, preEraStore.flatKV, "a pre-era height must not open flatkv")
	require.Equal(t, types.MemiavlOnly, preEraStore.currentWriteMode)
	val, found, err := preEraStore.Get(keys.BankStoreKey, []byte("k"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, valAt(3), val, "value as-of height 3")
	require.NoError(t, preEraStore.Close())

	// In-era height: flatkv must still be opened, from the same never-loaded store.
	inEra, err := fresh.LoadVersionReadOnly(7)
	require.NoError(t, err)
	inEraStore, ok := inEra.(*CompositeCommitStore)
	require.True(t, ok)
	defer func() { _ = inEraStore.Close() }()
	require.NotNil(t, inEraStore.flatKV, "in-era heights must keep loading flatkv")
}

// TestDerivedStoreRefusesLoads pins that a store which does not own the data directory rejects every load.
// Adopting a view and continuing to load through it used to depend on the view's backend mix to fail: a
// flatkv-carrying view errored ("store is read-only") but a memiavl-only view silently succeeded, leaving reads
// served from the version the view was built at. This fixture is deliberately memiavl-only, which is the
// configuration that was silent.
func TestDerivedStoreRefusesLoads(t *testing.T) {
	dir := t.TempDir()
	cfg := autoExportConfig()

	cs := openAutoStoreWithConfig(t, dir, cfg, 100)
	for i := 1; i <= 3; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte{byte(0x10 + i)}},
			}}},
		}))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.Nil(t, cs.flatKV, "fixture precondition: flatkv must not be materialized")
	require.NoError(t, cs.Close())

	fresh, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	defer func() { _ = fresh.Close() }()
	require.NoError(t, fresh.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	view, err := fresh.LoadVersion(2, false)
	require.NoError(t, err)
	viewStore, ok := view.(*CompositeCommitStore)
	require.True(t, ok)
	defer func() { _ = viewStore.Close() }()
	require.True(t, viewStore.derived, "a read-only view must be marked derived")

	// Every load path must refuse, so a caller that adopted the view cannot keep loading through it.
	require.ErrorIs(t, viewStore.LoadLatest(), errDerivedStore)
	_, err = viewStore.LoadVersion(0, false)
	require.ErrorIs(t, err, errDerivedStore)
	_, err = viewStore.LoadVersion(2, false)
	require.ErrorIs(t, err, errDerivedStore)
	_, err = viewStore.LoadVersionReadOnly(0)
	require.ErrorIs(t, err, errDerivedStore)

	// The view still serves the height it was built at.
	require.Equal(t, int64(2), viewStore.Version())
}
