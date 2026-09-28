package rootmulti

import (
	"bytes"
	"testing"

	protoio "github.com/gogo/protobuf/io"
	snapshottypes "github.com/sei-protocol/sei-chain/sei-cosmos/snapshots/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

// TestRestoreRejectsNodeOutsideStoreSection feeds Restore a stream whose first
// node is not preceded by a named store item. Restore must return an error
// instead of handing the node to an importer that has no open store.
func TestRestoreRejectsNodeOutsideStoreSection(t *testing.T) {
	leaf := snapshottypes.SnapshotItem{Item: &snapshottypes.SnapshotItem_IAVL{
		IAVL: &snapshottypes.SnapshotIAVLItem{Key: []byte("k"), Value: []byte("v"), Version: 1},
	}}
	unnamedStore := snapshottypes.SnapshotItem{Item: &snapshottypes.SnapshotItem_Store{
		Store: &snapshottypes.SnapshotStoreItem{Name: ""},
	}}

	configs := map[string]seidbconfig.StateCommitConfig{
		"memiavl_only":    memiavlOnlyConfig(),
		"test_dual_write": dualWriteConfig(),
	}
	streams := map[string][]snapshottypes.SnapshotItem{
		"no store item":      {leaf},
		"unnamed store item": {unnamedStore, leaf},
	}

	for cfgName, cfg := range configs {
		for streamName, items := range streams {
			t.Run(cfgName+"/"+streamName, func(t *testing.T) {
				var buf bytes.Buffer
				writer := protoio.NewDelimitedWriter(&buf)
				for i := range items {
					require.NoError(t, writer.WriteMsg(&items[i]))
				}

				store, _ := newTestRootMulti(t, t.TempDir(), cfg)
				reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
				require.NotPanics(t, func() {
					_, err := store.Restore(1, snapshottypes.CurrentFormat, reader)
					require.ErrorContains(t, err, "outside a named store section")
				})
			})
		}
	}
}
