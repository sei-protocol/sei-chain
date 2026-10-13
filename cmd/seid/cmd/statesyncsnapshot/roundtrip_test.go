package statesyncsnapshot

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

func testLayout(t *testing.T) node.AutobahnLayout {
	psd := t.TempDir()
	return node.AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          filepath.Join(psd, "data", "state_commit", "flatkv"),
		EpochsDir:          filepath.Join(psd, "epochs"),
		BlockDBDir:         filepath.Join(psd, "blockdb"),
		ReceiptStoreDir:    filepath.Join(psd, "data", "ledger", "receipt"),
		StateStoreDir:      filepath.Join(psd, "data", "state_store"),
		HashVaultDir:       filepath.Join(psd, "hashvault"),
		RestoredMarker:     filepath.Join(psd, "state-sync-restored"),
	}
}

func storeConfig(t *testing.T, dir string) *config.Config {
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dir
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 2
	return cfg
}

func commitBlock(t *testing.T, s *flatkv.CommitStore, n byte) int64 {
	var addr ktype.Address
	addr[19] = n
	var slot ktype.Slot
	slot[31] = n
	nonce := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(nonce, uint64(n))
	var codeHash vtype.CodeHash
	codeHash[0] = n
	value := make([]byte, 32)
	value[31] = n
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: nonce},
			{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: codeHash[:]},
			{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60, n}},
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: value},
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte{'b', n}, Value: []byte{n}}}}},
	}))
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, s.FlushSnapshots())
	return v
}

func openStore(t *testing.T, dir string) *flatkv.CommitStore {
	cfg := storeConfig(t, dir)
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	return s
}

func TestCreateRestoreRoundTrip(t *testing.T) {
	src := target{layout: testLayout(t), chainID: "chain-a", mode: tmcfg.ModeFull}
	s := openStore(t, src.layout.FlatKVDir)
	commitBlock(t, s, 1)
	h := commitBlock(t, s, 2)
	require.Eventually(t, func() bool {
		v, err := flatkv.ActiveSnapshotVersion(src.layout.FlatKVDir)
		return err == nil && v == h
	}, timeout, tick)
	require.NoError(t, os.MkdirAll(src.layout.EpochsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src.layout.EpochsDir, "registry_a.pb"), []byte("epoch-a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src.layout.EpochsDir, "registry_b.pb"), nil, 0o600))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(src.layout.EpochsDir, "registry_a.pb"), old, old))

	var archive, stderr bytes.Buffer
	require.NoError(t, runCreate(t.Context(), src, "-", 0, &archive, &stderr))
	require.Equal(t, `{"height":`+itoa(h)+"}\n", firstLine(stderr.String()))
	now := time.Now()
	require.NoError(t, os.Chtimes(filepath.Join(src.layout.EpochsDir, "registry_a.pb"), now, now))
	require.ErrorContains(t, runCreate(t.Context(), src, "-", 0, io.Discard, io.Discard), "changed after checkpoint")
	require.NoError(t, s.Close())

	dst := target{layout: testLayout(t), chainID: "chain-a", mode: tmcfg.ModeFull}
	var restoreErr bytes.Buffer
	require.NoError(t, runRestore(t.Context(), dst, "-", bytes.NewReader(archive.Bytes()), &restoreErr))
	require.Contains(t, restoreErr.String(), `{"height":`+itoa(h)+"}")
	require.FileExists(t, dst.layout.RestoredMarker)
	got, err := os.ReadFile(filepath.Join(dst.layout.EpochsDir, "registry_a.pb"))
	require.NoError(t, err)
	require.Equal(t, "epoch-a", string(got))
	require.NoDirExists(t, filepath.Join(dst.layout.PersistentStateDir, restoreStagingDir))

	restored := openStore(t, dst.layout.FlatKVDir)
	require.Equal(t, h, restored.Version())
	v, ok := restored.Get("bank", []byte{'b', 2})
	require.True(t, ok)
	require.Equal(t, []byte{2}, v)
	require.ErrorContains(t, runRestore(t.Context(), dst, "-", bytes.NewReader(archive.Bytes()), &restoreErr), "is a node running")
	require.NoError(t, restored.Close())

	again := target{layout: dst.layout, chainID: "chain-a", mode: tmcfg.ModeFull}
	require.ErrorContains(t, runRestore(t.Context(), again, "-", bytes.NewReader(archive.Bytes()), &restoreErr), "not empty")

	other := target{layout: testLayout(t), chainID: "chain-b", mode: tmcfg.ModeFull}
	require.ErrorContains(t, runRestore(t.Context(), other, "-", bytes.NewReader(archive.Bytes()), &restoreErr), "chain id")
	require.NoFileExists(t, other.layout.RestoredMarker)
	require.NoFileExists(t, filepath.Join(other.layout.EpochsDir, "registry_a.pb"))

	truncated := target{layout: testLayout(t), chainID: "chain-a", mode: tmcfg.ModeFull}
	require.Error(t, runRestore(t.Context(), truncated, "-", bytes.NewReader(archive.Bytes()[:archive.Len()/2]), &restoreErr))
	require.NoFileExists(t, truncated.layout.RestoredMarker)
	empty, err := dirEmptyOrMissing(filepath.Join(truncated.layout.FlatKVDir))
	require.NoError(t, err)
	if !empty {
		entries, _ := os.ReadDir(truncated.layout.FlatKVDir)
		for _, e := range entries {
			require.Equal(t, "LOCK", e.Name(), "only the root lock file may remain")
		}
	}
}

const (
	timeout = 10 * time.Second
	tick    = 20 * time.Millisecond
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func firstLine(s string) string {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return s
	}
	return s[:i+1]
}
