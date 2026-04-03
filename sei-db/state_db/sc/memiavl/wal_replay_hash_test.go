package memiavl

import (
	"testing"

	proto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func cloneCommitInfo(ci *proto.CommitInfo) proto.CommitInfo {
	infos := make([]proto.StoreInfo, len(ci.StoreInfos))
	for i, si := range ci.StoreInfos {
		h := make([]byte, len(si.CommitId.Hash))
		copy(h, si.CommitId.Hash)
		infos[i] = proto.StoreInfo{
			Name: si.Name,
			CommitId: proto.CommitID{
				Version: si.CommitId.Version,
				Hash:    h,
			},
		}
	}
	return proto.CommitInfo{Version: ci.Version, StoreInfos: infos}
}

func TestWALReplayProducesIdenticalHashes(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Dir:             dir,
		CreateIfMissing: true,
		ZeroCopy:        false,
		Config: Config{
			AsyncCommitBuffer:       0,
			SnapshotInterval:        0,
			SnapshotMinTimeInterval: 0,
		},
	}

	db, err := OpenDB(0, opts)
	require.NoError(t, err)

	initialStores := []string{"acc", "bank", "distribution", "staking", "ibc", "upgrade"}
	var upgrades []*proto.TreeNameUpgrade
	for _, name := range initialStores {
		upgrades = append(upgrades, &proto.TreeNameUpgrade{Name: name})
	}
	require.NoError(t, db.ApplyUpgrades(upgrades))

	type commitRecord struct {
		version    int64
		commitInfo proto.CommitInfo
	}
	var records []commitRecord

	for block := 1; block <= 5; block++ {
		switch block {
		case 1:
			require.NoError(t, db.ApplyChangeSet("acc", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("acct1"), Value: []byte("balance100")}},
			}))
			require.NoError(t, db.ApplyChangeSet("bank", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("supply"), Value: []byte("1000")}},
			}))
			require.NoError(t, db.ApplyChangeSet("distribution", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("rewards1"), Value: []byte("10")}},
			}))
			require.NoError(t, db.ApplyChangeSet("staking", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("validator1"), Value: []byte("power100")}},
			}))
		case 2, 3, 4, 5:
			require.NoError(t, db.ApplyChangeSet("distribution", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("rewards1"), Value: []byte("updated_" + string(rune('0'+block)))}},
			}))
			require.NoError(t, db.ApplyChangeSet("staking", proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("validator1"), Value: []byte("power_" + string(rune('0'+block)))}},
			}))
		}

		v, err := db.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(block), v)

		ci := cloneCommitInfo(db.LastCommitInfo())
		records = append(records, commitRecord{version: v, commitInfo: ci})
		t.Logf("COMMIT version=%d stores=%d", ci.Version, len(ci.StoreInfos))
		for _, si := range ci.StoreInfos {
			t.Logf("  store=%s ver=%d hash=%X", si.Name, si.CommitId.Version, si.CommitId.Hash)
		}
	}

	require.NoError(t, db.Close())

	for _, rec := range records {
		t.Logf("--- Checking version %d ---", rec.version)

		readOpts := opts
		readOpts.ReadOnly = true
		readOpts.CreateIfMissing = false

		roDB, err := OpenDB(rec.version, readOpts)
		require.NoError(t, err)

		roCI := roDB.LastCommitInfo()

		t.Logf("REPLAY version=%d stores=%d", roCI.Version, len(roCI.StoreInfos))
		for _, si := range roCI.StoreInfos {
			t.Logf("  store=%s ver=%d hash=%X", si.Name, si.CommitId.Version, si.CommitId.Hash)
		}

		require.Equal(t, rec.commitInfo.Version, roCI.Version, "version mismatch at height %d", rec.version)
		require.Equal(t, len(rec.commitInfo.StoreInfos), len(roCI.StoreInfos), "store count mismatch at height %d", rec.version)

		commitMap := make(map[string]proto.StoreInfo)
		for _, si := range rec.commitInfo.StoreInfos {
			commitMap[si.Name] = si
		}
		for _, si := range roCI.StoreInfos {
			origSI, ok := commitMap[si.Name]
			require.True(t, ok, "store %s not found in commit at height %d", si.Name, rec.version)
			require.Equal(t, origSI.CommitId.Version, si.CommitId.Version, "version mismatch for store %s at height %d", si.Name, rec.version)
			require.Equalf(t, origSI.CommitId.Hash, si.CommitId.Hash, "HASH MISMATCH for store %s at height %d", si.Name, rec.version)
		}

		require.NoError(t, roDB.Close())
	}
}
