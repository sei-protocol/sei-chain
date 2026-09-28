package rootmulti

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	protoio "github.com/gogo/protobuf/io"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
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

// TestRestoreRejectsMalformedStream feeds Restore streams a peer can forge. Each must fail with an
// error rather than panic.
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
			wantErr: "outside a named store section",
		},
		"branch node before its leaves": {
			items:   []snapshottypes.SnapshotItem{storeItem("bank"), nodeItem(1, "k")},
			wantErr: "pending children",
		},
	}

	for cfgName, cfg := range configs {
		for streamName, stream := range streams {
			t.Run(cfgName+"/"+streamName, func(t *testing.T) {
				store, _ := newTestRootMulti(t, t.TempDir(), cfg)
				err := restoreWithin(t, store, snapshotStream(t, stream.items))
				require.ErrorContains(t, err, stream.wantErr)
			})
		}
	}
}

// failingStateStore is a state store whose Import fails, either at once or after reading every node.
type failingStateStore struct {
	seidbtypes.StateStore
	drain bool
}

func (f failingStateStore) Import(_ int64, ch <-chan seidbtypes.SnapshotNode) error {
	if f.drain {
		for range ch {
		}
	}
	return errors.New("import failed")
}

func (failingStateStore) SetEarliestVersion(int64, bool) error { return nil }

func (failingStateStore) SetLatestVersion(int64) error { return nil }

// TestRestoreReportsStateStoreImportFailure pins that a failed state-store import fails the restore,
// whether Import returns early with the stream still being sent or after reading all of it.
func TestRestoreReportsStateStoreImportFailure(t *testing.T) {
	leaves := func(n int) []snapshottypes.SnapshotItem {
		items := []snapshottypes.SnapshotItem{storeItem("bank")}
		for i := 0; i < n; i++ {
			items = append(items, nodeItem(0, fmt.Sprintf("k%05d", i)))
		}
		return items
	}
	cases := map[string]struct {
		drain bool
		items []snapshottypes.SnapshotItem
	}{
		// More leaves than the import buffer holds, so sending blocks once Import has stopped reading.
		"returns early": {drain: false, items: leaves(20000)},
		// A single leaf is a complete tree, so only the state store fails.
		"returns after draining": {drain: true, items: leaves(1)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store, _ := newTestRootMulti(t, t.TempDir(), memiavlOnlyConfig())
			store.ssStore = failingStateStore{drain: tc.drain}
			err := restoreWithin(t, store, snapshotStream(t, tc.items))
			require.ErrorContains(t, err, "state store import")
		})
	}
}
