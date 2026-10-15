package statesyncsnapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
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

const testChainID = "chain-a"

var errStopped = errors.New("stopped by the test")

func testLayout(psd string) node.AutobahnLayout {
	return node.AutobahnLayout{
		PersistentStateDir:    psd,
		FlatKVDir:             filepath.Join(psd, "data", "flatkv"),
		EpochRegistryFiles:    []string{filepath.Join(psd, "epochs", "registry_a.pb"), filepath.Join(psd, "epochs", "registry_b.pb")},
		BlockDBDirs:           []string{filepath.Join(psd, "blockdb", "0")},
		ReceiptStoreDir:       filepath.Join(psd, "data", "receipt"),
		StateStoreDir:         filepath.Join(psd, "data", "state_store"),
		StateStoreSnapshotDir: filepath.Join(psd, "data", "state_store-snapshots"),
		HashVaultDir:          filepath.Join(psd, "hashvault"),
		RestoreMarkerFile:     filepath.Join(psd, "state-sync-restored"),
	}
}

func targetAt(psd string) target {
	return target{layout: testLayout(psd), chainID: testChainID, mode: tmcfg.ModeFull}
}

// freshHome returns a fullnode home that holds only an empty FlatKV root with its lock file.
func freshHome(t *testing.T) target {
	t.Helper()
	tg := targetAt(t.TempDir())
	lock, err := flatkv.LockRoot(tg.layout.FlatKVDir)
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())
	return tg
}

// producer is a home with a FlatKV store that writes a checkpoint every block.
type producer struct {
	target
	store *flatkv.CommitStore
	rows  map[string][]byte
	// rowsAt holds a copy of rows at each committed height.
	rowsAt map[int64]map[string][]byte
}

// newProducer opens a store that keeps keepRecent older checkpoints, and writes epoch files with an
// mtime an hour ago.
func newProducer(t *testing.T, keepRecent uint32) *producer {
	t.Helper()
	p := &producer{target: targetAt(t.TempDir()), rows: map[string][]byte{}, rowsAt: map[int64]map[string][]byte{}}
	p.store = openStore(t, p.layout.FlatKVDir, keepRecent)
	t.Cleanup(func() {
		if p.store != nil {
			_ = p.store.Close()
		}
	})
	writeEpochFiles(t, p.layout, []byte("epoch-a"), nil, time.Now().Add(-time.Hour))
	return p
}

// closeStore closes the producer's store, so the home no longer changes.
func (p *producer) closeStore(t *testing.T) {
	t.Helper()
	require.NoError(t, p.store.Close())
	p.store = nil
}

// commit commits one block that writes to all four data databases and to the bank module, and waits
// for its checkpoint to become current.
func (p *producer) commit(t *testing.T, n byte) int64 {
	t.Helper()
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
	evmPairs := []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: nonce},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: codeHash[:]},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: []byte{0x60, n}},
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Value: value},
	}
	bankPairs := []*proto.KVPair{{Key: []byte{'b', n}, Value: []byte{n}}}
	require.NoError(t, p.store.ApplyChangeSets(p.store.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: bankPairs}},
	}))
	v, err := p.store.Commit(p.store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, p.store.FlushSnapshots())
	require.Eventually(t, func() bool {
		cur, err := flatkv.ActiveSnapshotVersion(p.layout.FlatKVDir)
		return err == nil && cur == v
	}, 10*time.Second, 20*time.Millisecond)
	for _, kv := range evmPairs {
		got, ok := p.store.Get("evm", kv.Key)
		require.True(t, ok)
		p.rows["evm/"+string(kv.Key)] = got
	}
	p.rows["bank/"+string(bankPairs[0].Key)] = bankPairs[0].Value
	p.rowsAt[v] = maps.Clone(p.rows)
	return v
}

// createArchive packs the producer's current checkpoint into a file and returns its path and height.
func (p *producer) createArchive(t *testing.T) (string, int64) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "archive.tar.zst")
	var stderr bytes.Buffer
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: out}, nil, &stderr))
	return out, heightLine(t, stderr.String())
}

func openStore(t *testing.T, dir string, keepRecent uint32) *flatkv.CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dir
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = keepRecent
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	return s
}

// requireRestored asserts that the home of tg loads at height with the producer's rows, and holds the
// producer's epoch files and the marker for height.
func requireRestored(t *testing.T, tg target, p *producer, height int64) {
	t.Helper()
	for i, path := range tg.layout.EpochRegistryFiles {
		want, err := os.ReadFile(p.layout.EpochRegistryFiles[i])
		require.NoError(t, err)
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	marker, err := os.ReadFile(tg.layout.RestoreMarkerFile)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(height, 10)+"\n", string(marker))
	require.NoDirExists(t, filepath.Join(tg.layout.PersistentStateDir, restoreStagingDirName))

	s := openStore(t, tg.layout.FlatKVDir, 2)
	defer func() { require.NoError(t, s.Close()) }()
	require.Equal(t, height, s.Version())
	for k, want := range p.rowsAt[height] {
		module, key, _ := bytes.Cut([]byte(k), []byte("/"))
		got, ok := s.Get(string(module), key)
		require.True(t, ok, "row %q", k)
		require.Equal(t, want, got, "row %q", k)
	}
}

func writeEpochFiles(t *testing.T, layout node.AutobahnLayout, a, b []byte, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(layout.EpochRegistryFiles[0]), 0o750))
	for i, data := range [][]byte{a, b} {
		p := layout.EpochRegistryFiles[i]
		require.NoError(t, os.WriteFile(p, data, 0o600))
		require.NoError(t, os.Chtimes(p, mtime, mtime))
	}
}

// heightLine returns H from the single line {"height":H} that out must consist of.
func heightLine(t *testing.T, out string) int64 {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix([]byte(out), []byte("\n")), []byte("\n"))
	var heights []int64
	for _, l := range lines {
		if h, ok := bytes.CutPrefix(l, []byte(`{"height":`)); ok {
			v, err := strconv.ParseInt(string(bytes.TrimSuffix(h, []byte("}"))), 10, 64)
			require.NoError(t, err)
			heights = append(heights, v)
		}
	}
	require.Len(t, heights, 1, "output %q", out)
	return heights[0]
}

// treeOf lists every path under root with its type, and the size and SHA-256 of each regular file.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			tree[rel] = "dir"
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			tree[rel] = "symlink " + target
		default:
			data, err := os.ReadFile(p) //nolint:gosec // test path.
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			tree[rel] = "file " + strconv.Itoa(len(data)) + " " + hex.EncodeToString(sum[:])
		}
		return nil
	}))
	return tree
}

// stopAt returns a hook that stops the command at step.
func stopAt(step string) stepHook {
	return func(s string) error {
		if s == step {
			return errStopped
		}
		return nil
	}
}
