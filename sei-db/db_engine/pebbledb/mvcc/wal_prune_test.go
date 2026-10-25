package mvcc

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestPruneWALBeforeVersionKeepsRecoveryFloor(t *testing.T) {
	for _, tc := range []struct {
		name         string
		snapshot     int64
		firstVersion int64
	}{
		{
			name:         "snapshot anchor keeps more history",
			snapshot:     100,
			firstVersion: 100,
		},
		{
			name:         "recovery floor keeps more history",
			snapshot:     1499,
			firstVersion: 501,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultStateStoreConfig()
			cfg.Backend = config.PebbleDBBackend
			cfg.SnapshotInterval = 100
			cfg.SnapshotKeepRecent = 1
			cfg.PruneIntervalSeconds = 3600

			store, err := OpenDB(filepath.Join(t.TempDir(), "state"), cfg)
			require.NoError(t, err)
			db := store.(*Database)
			t.Cleanup(func() { require.NoError(t, db.Close()) })

			for version := int64(1); version <= 1500; version++ {
				require.NoError(t, db.streamHandler.Write(proto.ChangelogEntry{Version: version}))
			}

			require.NoError(t, db.PruneWALBeforeVersion(tc.snapshot))
			firstOffset, err := db.streamHandler.FirstOffset()
			require.NoError(t, err)
			first, err := db.streamHandler.ReadAt(firstOffset)
			require.NoError(t, err)
			require.Equal(t, tc.firstVersion, first.Version)
		})
	}
}
