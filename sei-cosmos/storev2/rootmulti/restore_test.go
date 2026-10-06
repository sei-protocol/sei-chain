package rootmulti

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	protoio "github.com/gogo/protobuf/io"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

func storeItem(name string) snapshottypes.SnapshotItem {
	return snapshottypes.SnapshotItem{Item: &snapshottypes.SnapshotItem_Store{
		Store: &snapshottypes.SnapshotStoreItem{Name: name},
	}}
}

func nodeItem(height int32, key string) snapshottypes.SnapshotItem {
	item := &snapshottypes.SnapshotIAVLItem{Key: []byte(key), Version: 1, Height: height}
	if height == 0 {
		item.Value = []byte("v")
	}
	return snapshottypes.SnapshotItem{Item: &snapshottypes.SnapshotItem_IAVL{IAVL: item}}
}

func snapshotStream(t *testing.T, items []snapshottypes.SnapshotItem) protoio.Reader {
	t.Helper()
	var buf bytes.Buffer
	writer := protoio.NewDelimitedWriter(&buf)
	for i := range items {
		require.NoError(t, writer.WriteMsg(&items[i]))
	}
	return protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
}

// restoreWithin runs Restore and fails the test if it panics or does not return in time.
func restoreWithin(t *testing.T, store *Store, reader protoio.Reader) error {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result <- fmt.Errorf("restore panicked: %v", r)
			}
		}()
		_, err := store.Restore(1, snapshottypes.CurrentFormat, reader)
		result <- err
	}()
	select {
	case err := <-result:
		require.NotContains(t, fmt.Sprint(err), "restore panicked")
		return err
	case <-time.After(60 * time.Second):
		t.Fatal("restore did not return")
		return nil
	}
}

// scSnapshotState lists memIAVL's snapshot directories under home and the one current points to.
type scSnapshotState struct {
	snapshots []string
	current   string
}

func readSCSnapshotState(t *testing.T, home string) scSnapshotState {
	t.Helper()
	dir := utils.GetCosmosSCStorePath(home)
	snapshots, err := filepath.Glob(filepath.Join(dir, "snapshot-*"))
	require.NoError(t, err)
	current, _ := os.Readlink(filepath.Join(dir, "current"))
	return scSnapshotState{snapshots: snapshots, current: current}
}

// TestRestoreRejectsMalformedStream feeds Restore streams that break the snapshot format. Each must fail
// with an error and leave memIAVL's published snapshots as they were.
func TestRestoreRejectsMalformedStream(t *testing.T) {
	configs := map[string]seidbconfig.StateCommitConfig{
		"memiavl_only":    memiavlOnlyConfig(),
		"test_dual_write": dualWriteConfig(),
	}
	streams := map[string]struct {
		items   []snapshottypes.SnapshotItem
		wantErr string
	}{
		"node before any store item": {
			items:   []snapshottypes.SnapshotItem{nodeItem(0, "k")},
			wantErr: "outside a named store section",
		},
		"node after an unnamed store item": {
			items:   []snapshottypes.SnapshotItem{storeItem(""), nodeItem(0, "k")},
			wantErr: "invalid snapshot module name",
		},
		"branch node before its leaves": {
			items:   []snapshottypes.SnapshotItem{storeItem("bank"), nodeItem(1, "k")},
			wantErr: "pending children",
		},
	}

	for cfgName, cfg := range configs {
		for streamName, stream := range streams {
			t.Run(cfgName+"/"+streamName, func(t *testing.T) {
				home := t.TempDir()
				store, _ := newTestRootMulti(t, home, cfg)
				before := readSCSnapshotState(t, home)
				err := restoreWithin(t, store, snapshotStream(t, stream.items))
				require.ErrorContains(t, err, stream.wantErr)
				require.Equal(t, before, readSCSnapshotState(t, home), "a failed restore must not publish a snapshot")
			})
		}
	}
}

// fakeStateStore is a state store whose Import returns importErr, either at once or after reading and
// recording every node, and which counts the version writes restore makes.
type fakeStateStore struct {
	seidbtypes.StateStore
	returnEarly   bool
	importErr     error
	imported      []seidbtypes.SnapshotNode
	versionWrites int
}

func (f *fakeStateStore) Import(_ int64, ch <-chan seidbtypes.SnapshotNode) error {
	if !f.returnEarly {
		for node := range ch {
			f.imported = append(f.imported, node)
		}
	}
	return f.importErr
}

func (f *fakeStateStore) SetEarliestVersion(int64, bool) error {
	f.versionWrites++
	return nil
}

func (f *fakeStateStore) SetLatestVersion(int64) error {
	f.versionWrites++
	return nil
}

func leafStream(n int) []snapshottypes.SnapshotItem {
	items := []snapshottypes.SnapshotItem{storeItem("bank")}
	for i := 0; i < n; i++ {
		items = append(items, nodeItem(0, fmt.Sprintf("k%05d", i)))
	}
	return items
}

// TestRestoreFailurePublishesNothing pins that a failed restore returns an error, publishes no memIAVL
// snapshot, and does not record the snapshot height on the state store, whether the state store or the
// SC stream failed.
func TestRestoreFailurePublishesNothing(t *testing.T) {
	importErr := errors.New("import failed")
	cases := map[string]struct {
		ss      *fakeStateStore
		items   []snapshottypes.SnapshotItem
		wantErr string
	}{
		// More leaves than the import buffer holds, so sending blocks once Import has stopped reading.
		"state store import returns early": {
			ss:      &fakeStateStore{returnEarly: true, importErr: importErr},
			items:   leafStream(20000),
			wantErr: "state store import",
		},
		// A single leaf is a complete tree, so only the state store fails.
		"state store import fails after draining": {
			ss:      &fakeStateStore{importErr: importErr},
			items:   leafStream(1),
			wantErr: "state store import",
		},
		"SC stream fails": {
			ss:      &fakeStateStore{},
			items:   []snapshottypes.SnapshotItem{storeItem("bank"), nodeItem(1, "k")},
			wantErr: "pending children",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			store, _ := newTestRootMulti(t, home, memiavlOnlyConfig())
			store.ssStore = tc.ss
			before := readSCSnapshotState(t, home)
			err := restoreWithin(t, store, snapshotStream(t, tc.items))
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, before, readSCSnapshotState(t, home), "a failed restore must not publish a snapshot")
			require.Zero(t, tc.ss.versionWrites, "a failed restore must not record the snapshot height")
		})
	}
}

// TestRestoreStopsWhenCommitStoreRejectsNode pins that a node the SC importer rejects fails the restore, and
// that the state store receives only the nodes accepted before it.
func TestRestoreStopsWhenCommitStoreRejectsNode(t *testing.T) {
	store, _ := newTestRootMulti(t, t.TempDir(), flatKVOnlyConfig())
	ss := &fakeStateStore{}
	store.ssStore = ss

	leaf := func(key string, version int64) snapshottypes.SnapshotItem {
		return snapshottypes.SnapshotItem{Item: &snapshottypes.SnapshotItem_IAVL{IAVL: &snapshottypes.SnapshotIAVLItem{
			Key:     []byte(key),
			Value:   vtype.SerializeMisc(1, []byte("v")),
			Version: version,
		}}}
	}
	items := []snapshottypes.SnapshotItem{storeItem(keys.FlatKVStoreKey), leaf("bank/a", 1), leaf("bank/b", 2)}

	err := restoreWithin(t, store, snapshotStream(t, items))
	require.ErrorContains(t, err, "the import is at version")
	require.Len(t, ss.imported, 1)
	require.Equal(t, []byte("bank/a"), ss.imported[0].Key)
	require.Zero(t, ss.versionWrites, "a failed restore must not record the snapshot height")
}

// TestRestoreSuccessPublishes pins that a successful restore publishes the memIAVL snapshot and records
// the snapshot height on the state store.
func TestRestoreSuccessPublishes(t *testing.T) {
	home := t.TempDir()
	store, _ := newTestRootMulti(t, home, memiavlOnlyConfig())
	ss := &fakeStateStore{}
	store.ssStore = ss
	require.NoError(t, store.scStore.Close())

	_, err := store.restore(1, snapshotStream(t, leafStream(1)))
	require.NoError(t, err)
	require.Equal(t, "snapshot-00000000000000000001", readSCSnapshotState(t, home).current)
	require.Equal(t, 2, ss.versionWrites)
}
