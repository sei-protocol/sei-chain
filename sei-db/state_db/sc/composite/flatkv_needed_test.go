package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TestFlatKVNeededAtHeight_NilHandleMeansNotMaterialized pins the cheapest branch: under types.Auto the
// constructor only builds flatkv when its directory already exists, so a nil handle is itself the answer and
// must not be treated as "unknown".
func TestFlatKVNeededAtHeight_NilHandleMeansNotMaterialized(t *testing.T) {
	needed, err := FlatKVNeededAtHeight(nil, types.Auto, 5)
	require.NoError(t, err)
	require.False(t, needed)
}

// TestFlatKVNeededAtHeight_FixedModeSkipsClassification confirms a non-Auto configured mode answers from
// config alone. The handle here fails every load, so reaching the classification path at all would surface as
// an error — that is the assertion.
func TestFlatKVNeededAtHeight_FixedModeSkipsClassification(t *testing.T) {
	needed, err := FlatKVNeededAtHeight(&failingEVMStore{}, types.EVMMigrated, 5)
	require.NoError(t, err)
	require.True(t, needed)
}

// TestFlatKVNeededAtHeight_LatestNeedsFlatKV covers height 0 (latest), which is in-era by definition whenever
// flatkv exists at all.
func TestFlatKVNeededAtHeight_LatestNeedsFlatKV(t *testing.T) {
	needed, err := FlatKVNeededAtHeight(&failingEVMStore{}, types.Auto, 0)
	require.NoError(t, err)
	require.True(t, needed)
}

// TestFlatKVNeededAtHeight_TipOpenFailureIsAnError pins the deliberate fail-loud choice. A flatkv whose tip
// cannot be opened is either mid-materialization or corrupt, and those are indistinguishable here. Degrading
// to "not needed" would serve the height from a memiavl whose migrated keys have been deleted, answering
// "absent" for keys that exist, so this must error instead.
func TestFlatKVNeededAtHeight_TipOpenFailureIsAnError(t *testing.T) {
	needed, err := FlatKVNeededAtHeight(&failingEVMStore{}, types.Auto, 5)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to open flatkv tip to classify height 5")
	require.False(t, needed, "the boolean must not be trusted alongside an error")
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
