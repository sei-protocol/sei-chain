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

// TestCorruptFlatKVDirFailsOnLoad pins where the corrupt-flatkv-directory tripwire lives. Under
// types.Auto the constructor treats directory presence as the signal that flatkv participates, so a
// directory that exists but cannot be opened must not be mistaken for "no flatkv here" — those keys
// were deleted out of memiavl, and continuing would answer "absent" for keys that exist.
//
// The constructor no longer reads the directory, so the failure surfaces when LoadLatest opens the
// DBs. That only holds for a working directory the open path will not silently rebuild: a working dir
// whose SNAPSHOT_BASE does not match the current snapshot is wiped and re-cloned by createWorkingDir,
// which repairs the damage rather than reporting it. This fixture commits first so SNAPSHOT_BASE is
// present and the re-clone is skipped.
func TestCorruptFlatKVDirFailsOnLoad(t *testing.T) {
	dir := t.TempDir()
	cs := openAutoStoreWithConfig(t, dir, autoExportConfig(), 100)
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	require.NoError(t, cs.ApplyChangeSets(nil))
	_, err := cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	// Replace the working misc DB with a regular file, leaving SNAPSHOT_BASE intact so the open path
	// reuses this working dir instead of re-cloning it.
	miscDir := filepath.Join(utils.GetFlatKVPath(dir), "working", "misc")
	require.NoError(t, os.RemoveAll(miscDir))
	require.NoError(t, os.WriteFile(miscDir, []byte("not a pebble db"), 0o600))

	reopened, err := NewCompositeCommitStore(t.Context(), dir, autoExportConfig())
	require.NoError(t, err, "construction does not open the DBs, so it cannot detect this")
	defer func() { _ = reopened.Close() }()

	require.Error(t, reopened.LoadLatest(), "a flatkv directory that cannot be opened must fail the load")
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
