package statesync

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const testChainID = "chain-a"

var errStopped = errors.New("stopped by the test")

func testLayout(psd string) node.AutobahnLayout {
	return node.AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          filepath.Join(psd, "data", "state_commit", "flatkv"),
		RestoreMarkerFile:  filepath.Join(psd, "state-sync-restore-pending"),
		RestoreProofFile:   filepath.Join(psd, "state-sync-proof"),
		StateSyncProofDir:  filepath.Join(psd, "data", "state_commit", "state_sync_proofs"),
		AppHashVaultDir:    filepath.Join(psd, "data", "ledger", "app_hash"),
		ImportStagingDir:   filepath.Join(psd, ".state-sync-import"),
	}
}

// freshHome returns a fullnode home with an empty persistent state dir.
func freshHome(t *testing.T) target {
	t.Helper()
	return target{
		layout:  testLayout(t.TempDir()),
		chainID: testChainID,
		mode:    tmcfg.ModeFull,
		genDoc:  &tmtypes.GenesisDoc{ChainID: testChainID, InitialHeight: 1},
	}
}

// producer is a FlatKV store that writes a checkpoint every block.
type producer struct {
	root  string
	store *flatkv.CommitStore
	// rowsAt holds the rows of each committed height, by module and key.
	rowsAt map[int64]map[string][]byte
	rows   map[string][]byte
}

func newProducer(t *testing.T) *producer {
	t.Helper()
	p := &producer{
		root:   filepath.Join(t.TempDir(), "flatkv"),
		rowsAt: map[int64]map[string][]byte{},
		rows:   map[string][]byte{},
	}
	p.store = openStore(t, p.root)
	t.Cleanup(func() { _ = p.store.Close() })
	return p
}

func openStore(t *testing.T, dir string) *flatkv.CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = dir
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 2
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	return s
}

// commit commits one block that writes to all four FlatKV databases, to the bank module and the
// epoch registry row of the restore fixture of the block, and waits for its checkpoint. It returns
// the height of the block.
func (p *producer) commit(t *testing.T, n byte) int64 {
	t.Helper()
	fixture := fixtureAt(t, p.store.Version()+1)
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
	registryPairs := []*proto.KVPair{{Key: fixture.Key, Value: fixture.Value}}
	require.NoError(t, p.store.ApplyChangeSets(p.store.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: bankPairs}},
		{Name: fixture.Module, Changeset: proto.ChangeSet{Pairs: registryPairs}},
	}))
	v, err := p.store.Commit(p.store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, p.store.FlushSnapshots())
	for _, kv := range evmPairs {
		got, ok := p.store.Get("evm", kv.Key)
		require.True(t, ok)
		p.rows["evm/"+string(kv.Key)] = got
	}
	p.rows["bank/"+string(bankPairs[0].Key)] = bankPairs[0].Value
	p.rows[fixture.Module+"/"+string(fixture.Key)] = fixture.Value
	rows := make(map[string][]byte, len(p.rows))
	for k, v := range p.rows {
		rows[k] = v
	}
	p.rowsAt[v] = rows
	return v
}

// stage returns a hard-linked copy of the checkpoint at height.
func (p *producer) stage(t *testing.T, height int64) string {
	t.Helper()
	staged := filepath.Join(t.TempDir(), "staged")
	require.NoError(t, flatkv.HardLinkSnapshot(p.root, height, staged))
	return staged
}

// archive returns the archive of the checkpoint at height of chain chainID, with a state sync
// proof file of height.
func (p *producer) archive(t *testing.T, chainID string, height int64) []byte {
	t.Helper()
	return archiveOf(t, p.stage(t, height), chainID, height)
}

// archiveOf returns the archive of the checkpoint in dir at height of chain chainID, with a state
// sync proof file of height.
func archiveOf(t *testing.T, dir string, chainID string, height int64) []byte {
	t.Helper()
	return archiveWithProof(t, dir, chainID, height, proofAt(t, height))
}

// archiveWithProof returns the archive of the checkpoint in dir at height of chain chainID, with
// the state sync proof file proof.
func archiveWithProof(t *testing.T, dir string, chainID string, height int64, proof []byte) []byte {
	t.Helper()
	m, err := ssarchive.BuildManifest(chainID, height, dir)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, ssarchive.WriteArchive(t.Context(), &out, m, proof, dir))
	return out.Bytes()
}

// fixtureAt returns the restore fixture of height: the epoch registry row that the producer
// commits at height, and a state sync proof file of height that verifies against it.
func fixtureAt(t *testing.T, height int64) node.StateSyncProofFixture {
	t.Helper()
	fixture, err := node.GenStateSyncProofFixture(utils.TestRngFromSeed(height), height)
	require.NoError(t, err)
	return fixture
}

// proofAt returns the state sync proof file of the restore fixture of height.
func proofAt(t *testing.T, height int64) []byte {
	t.Helper()
	return fixtureAt(t, height).Proof
}

func bytesSource(data []byte) source {
	return source{
		name: "test archive",
		open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	}
}

// requireRestored asserts that the home of tg holds the rows of p at height in FlatKV, the restore
// marker of height, the state sync proof file of height, the restore anchor of height, and no
// staging dir.
func requireRestored(t *testing.T, tg target, p *producer, height int64) {
	t.Helper()
	marker, err := os.ReadFile(tg.layout.RestoreMarkerFile)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%d\n", height), string(marker))
	proof, err := os.ReadFile(tg.layout.RestoreProofFile)
	require.NoError(t, err)
	require.Equal(t, proofAt(t, height), proof)
	anchor, err := node.ReadNewestAppHashForTesting(tg.layout, tg.genDoc)
	require.NoError(t, err)
	require.True(t, anchor.IsRestoreAnchor())
	require.Equal(t, uint64(height), anchor.BlockHeight()) //nolint:gosec // test heights are positive.
	require.Equal(t, fixtureAt(t, height).AppHash, anchor.AppHash())
	require.NoDirExists(t, tg.layout.ImportStagingDir)

	s := openStore(t, tg.layout.FlatKVDir)
	defer func() { require.NoError(t, s.Close()) }()
	requireRows(t, s, p, height)
}

// requireRows asserts that s is at height and holds the rows of p at height.
func requireRows(t *testing.T, s *flatkv.CommitStore, p *producer, height int64) {
	t.Helper()
	require.Equal(t, height, s.Version())
	for k, want := range p.rowsAt[height] {
		module, key, _ := bytes.Cut([]byte(k), []byte("/"))
		got, ok := s.Get(string(module), key)
		require.True(t, ok, "row %q", k)
		require.Equal(t, want, got, "row %q", k)
	}
}

// requireUntouched asserts that a failed import left the home of tg without FlatKV data, app-hash
// vault, restore marker or staging dir.
func requireUntouched(t *testing.T, tg target) {
	t.Helper()
	require.NoFileExists(t, tg.layout.RestoreMarkerFile)
	require.NoDirExists(t, tg.layout.AppHashVaultDir)
	require.NoDirExists(t, tg.layout.ImportStagingDir)
	lock, err := flatkv.LockRoot(tg.layout.FlatKVDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Unlock()) }()
	empty, err := lock.Empty()
	require.NoError(t, err)
	require.True(t, empty)
}

// memObject is one object of a memStore.
type memObject struct {
	data    []byte
	version string
}

// memStore is an ObjectStore in memory.
type memStore struct {
	mu       sync.Mutex
	objects  map[string]memObject
	versions int
}

func newMemStore() *memStore {
	return &memStore{objects: map[string]memObject{}}
}

func (s *memStore) Put(_ context.Context, key string, body io.Reader, size int64, pre ssarchive.Precondition) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("put %s: body is %d bytes, size is %d", key, len(data), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if (pre.Absent && ok) || (pre.Version != "" && (!ok || obj.version != pre.Version)) {
		return fmt.Errorf("put %s: %w", key, ssarchive.ErrPreconditionFailed)
	}
	s.versions++
	s.objects[key] = memObject{data: data, version: fmt.Sprint(s.versions)}
	return nil
}

func (s *memStore) Get(_ context.Context, key string) (io.ReadCloser, ssarchive.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return nil, ssarchive.ObjectInfo{}, fmt.Errorf("%s: %w", key, ssarchive.ErrObjectNotFound)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), ssarchive.ObjectInfo{Version: obj.version}, nil
}

func (s *memStore) Stat(_ context.Context, key string) (ssarchive.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return ssarchive.ObjectInfo{}, fmt.Errorf("%s: %w", key, ssarchive.ErrObjectNotFound)
	}
	return ssarchive.ObjectInfo{Version: obj.version}, nil
}
