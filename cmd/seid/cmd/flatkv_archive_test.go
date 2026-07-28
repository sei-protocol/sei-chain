package cmd

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	sscomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/stretchr/testify/require"
)

func makeFlatKVSnapshots(t *testing.T, root string, heights ...int64) {
	t.Helper()
	for _, h := range heights {
		require.NoError(t, os.MkdirAll(filepath.Join(root, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, h)), 0o755))
	}
}

func makeSSCheckpoints(t *testing.T, stateStoreDir string, versions ...int64) {
	t.Helper()
	for _, v := range versions {
		dir := filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(v))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cosmos", "pebbledb"), 0o755))
	}
}

func TestSelectStateStoreSourcePrefersNewestCheckpoint(t *testing.T) {
	stateStoreDir := t.TempDir()
	makeSSCheckpoints(t, stateStoreDir, 100, 200)

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Equal(t, sscomposite.CheckpointDirName(200), name)
	require.Equal(t, int64(200), version)
	require.Equal(t,
		filepath.Join(stateStoreDir, sscomposite.CheckpointsDirName, sscomposite.CheckpointDirName(200)),
		source)
}

func TestSelectStateStoreSourceFallsBackToLiveDir(t *testing.T) {
	stateStoreDir := t.TempDir()

	source, name, version, err := selectStateStoreSource(stateStoreDir)
	require.NoError(t, err)
	require.Empty(t, name)
	require.Zero(t, version)
	require.Equal(t, stateStoreDir, source)
}

func TestSelectFlatKVSnapshotAtMost(t *testing.T) {
	root := t.TempDir()
	makeFlatKVSnapshots(t, root, 100, 200, 300)

	// The state-store checkpoint's label bounds the FlatKV height from above:
	// pairing H <= label is what guarantees the restored query store is
	// complete up to the archive height.
	dir, name, height, err := selectFlatKVSnapshotAtMost(root, 250)
	require.NoError(t, err)
	require.Equal(t, int64(200), height)
	require.Equal(t, fmt.Sprintf("%s%020d", flatKVArchiveSnapshotPref, int64(200)), name)
	require.Equal(t, filepath.Join(root, name), dir)

	// Exact match is allowed.
	_, _, height, err = selectFlatKVSnapshotAtMost(root, 300)
	require.NoError(t, err)
	require.Equal(t, int64(300), height)

	// No snapshot at or below the checkpoint label: refuse rather than ship
	// an archive whose query store has holes below the archive height.
	_, _, _, err = selectFlatKVSnapshotAtMost(root, 50)
	require.Error(t, err)
}

// TestRebuildStateStoreFromFlatKV covers the single-source-of-truth restore
// path: commit cosmos-module and EVM data into a FlatKV store, rebuild a
// fresh state_store from it, and verify every logical entry is queryable at
// the archive height through the SS read API.
func TestRebuildStateStoreFromFlatKV(t *testing.T) {
	home := t.TempDir()
	flatKVRoot := filepath.Join(home, "data", "state_commit", "flatkv")

	bankKey := []byte("balances/addr1")
	bankValue := []byte("100usei")
	stakingKey := []byte("validators/val1")
	stakingValue := []byte("validator-record")
	var addr ktype.Address
	addr[19] = 0x42
	var slot ktype.Slot
	slot[31] = 0x01
	storageValue := make([]byte, 32)
	storageValue[31] = 0x07
	storageKey := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	fcfg := flatkvconfig.DefaultConfig()
	fcfg.DataDir = flatKVRoot
	store, err := flatkv.NewCommitStore(t.Context(), fcfg)
	require.NoError(t, err)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: bankKey, Value: bankValue},
		}}},
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: stakingKey, Value: stakingValue},
		}}},
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: storageKey, Value: storageValue},
		}}},
	}))
	height, err := store.Commit()
	require.NoError(t, err)
	require.NoError(t, store.Close())

	ssConfig := seidbconfig.DefaultStateStoreConfig()

	// A height mismatch must refuse to open rather than mislabel versions.
	_, err = openRestoredFlatKV(t.Context(), flatKVRoot, height+1)
	require.Error(t, err)

	restored, err := openRestoredFlatKV(t.Context(), flatKVRoot, height)
	require.NoError(t, err)
	defer func() { require.NoError(t, restored.Close()) }()

	// The restore-time content check: the full re-scan of the installed rows
	// must reproduce the checkpoint's committed LtHash.
	require.NoError(t, flatkv.VerifyLtHash(restored))

	entries, err := rebuildStateStoreFromFlatKV(home, restored, height, ssConfig)
	require.NoError(t, err)
	require.Equal(t, int64(3), entries)

	ssStore, err := ss.NewStateStore(home, ssConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, ssStore.Close()) }()

	got, err := ssStore.Get("bank", height, bankKey)
	require.NoError(t, err)
	require.Equal(t, bankValue, got)
	got, err = ssStore.Get("staking", height, stakingKey)
	require.NoError(t, err)
	require.Equal(t, stakingValue, got)
	got, err = ssStore.Get("evm", height, storageKey)
	require.NoError(t, err)
	require.Equal(t, storageValue, got)

	require.Equal(t, height, ssStore.GetLatestVersion())
	require.Equal(t, height, ssStore.GetEarliestVersion())
}

// writeTestArchive hand-rolls a tar.zst with the given manifest and raw
// entries so tests can construct shapes writeFlatKVArchive would never
// produce (e.g. entries the manifest does not list).
func writeTestArchive(t *testing.T, path string, manifest *flatKVArchiveManifest, entries map[string][]byte) {
	t.Helper()
	out, err := os.Create(path)
	require.NoError(t, err)
	zw, err := zstd.NewWriter(out)
	require.NoError(t, err)
	tw := tar.NewWriter(zw)

	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: flatKVArchiveManifestName, Mode: 0o644, Size: int64(len(manifestBytes)),
	}))
	_, err = tw.Write(manifestBytes)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(entries[name])),
		}))
		_, err = tw.Write(entries[name])
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	require.NoError(t, out.Close())
}

// TestExtractFlatKVArchiveRejectsUnlistedEntries pins the bidirectional
// manifest check: an archive entry the manifest does not list would otherwise
// be extracted and installed (the install step renames whole directories)
// without any hash covering it.
func TestExtractFlatKVArchiveRejectsUnlistedEntries(t *testing.T) {
	listedName := "flatkv/snapshot-00000000000000000001/misc/000001.sst"
	listedBody := []byte("listed-content")
	sum := sha256.Sum256(listedBody)
	manifest := &flatKVArchiveManifest{
		FormatVersion: flatKVArchiveFormatVersion,
		ChainID:       "test-chain",
		Height:        1,
		SnapshotName:  "snapshot-00000000000000000001",
		Files: []flatKVArchiveFileEntry{
			{Path: listedName, Size: int64(len(listedBody)), Mode: 0o644, SHA256: hex.EncodeToString(sum[:])},
		},
	}

	// Positive control: an archive matching its manifest extracts cleanly.
	goodPath := filepath.Join(t.TempDir(), "good.tar.zst")
	writeTestArchive(t, goodPath, manifest, map[string][]byte{listedName: listedBody})
	_, err := extractFlatKVArchive(goodPath, t.TempDir())
	require.NoError(t, err)

	// The same archive with one extra, unlisted entry must be rejected.
	badPath := filepath.Join(t.TempDir(), "bad.tar.zst")
	writeTestArchive(t, badPath, manifest, map[string][]byte{
		listedName: listedBody,
		"flatkv/snapshot-00000000000000000001/misc/999999.sst": []byte("smuggled"),
	})
	_, err = extractFlatKVArchive(badPath, t.TempDir())
	require.ErrorContains(t, err, "not listed in the manifest")
}

func TestArchiveHasStateStore(t *testing.T) {
	require.False(t, archiveHasStateStore(&flatKVArchiveManifest{Files: []flatKVArchiveFileEntry{
		{Path: "flatkv/snapshot-1/misc/000001.sst"},
	}}))
	require.True(t, archiveHasStateStore(&flatKVArchiveManifest{Files: []flatKVArchiveFileEntry{
		{Path: "flatkv/snapshot-1/misc/000001.sst"},
		{Path: "state_store/cosmos/pebbledb/000001.sst"},
	}}))
}
