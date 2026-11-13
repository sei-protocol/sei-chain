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

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const testChainID = "chain-a"

var errStopped = errors.New("stopped by the test")

func testLayout(psd string) node.AutobahnLayout {
	return node.AutobahnLayout{
		PersistentStateDir: psd,
		FlatKVDir:          filepath.Join(psd, "data", "flatkv"),
		BlockDBDirs:        []string{filepath.Join(psd, "blockdb", "0")},
		ReceiptStoreDir:    filepath.Join(psd, "data", "receipt"),
		StateStoreDir:      filepath.Join(psd, "data", "state_store"),
		AppHashVaultDir:    filepath.Join(psd, "data", "ledger", "app_hash"),
		RestoreMarkerFile:  filepath.Join(psd, "state-sync-restored"),
		RestoreProofFile:   filepath.Join(psd, "state-sync-proof"),
		StateSyncProofDir:  filepath.Join(psd, "data", "state_commit", "state_sync_proofs"),
		EpochRegistryDir:   filepath.Join(psd, "epochs"),
	}
}

// validatorKey returns the key of the one genesis validator of the test chain.
func validatorKey() atypes.SecretKey {
	return atypes.SecretKeyFromED25519(ed25519.TestSecretKey([]byte("validator")))
}

func testEpochGenesis() node.EpochGenesis {
	return node.EpochGenesis{
		GenDoc:     &tmtypes.GenesisDoc{ChainID: testChainID, InitialHeight: 1},
		Validators: []atypes.PublicKey{validatorKey().Public()},
	}
}

// stateSyncProofAt returns a state sync proof file for road height, signed by the genesis validator.
func stateSyncProofAt(t *testing.T, height int64) []byte {
	t.Helper()
	key := validatorKey()
	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1})
	require.NoError(t, err)
	road := atypes.RoadIndex(height) //nolint:gosec // test heights are positive.
	ep := atypes.NewEpoch(0, atypes.RoadRange{First: road, Next: road + 1}, committee)
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Index: road}, 1)
	prepareQC := atypes.BuildPrepareQC([]atypes.SecretKey{key}, proposal)
	commitQC := atypes.NewCommitQC([]*atypes.Signed[*atypes.CommitVote]{atypes.Sign(key, atypes.NewCommitVote(prepareQC))})
	roadBlock := atypes.NewRoadBlock(atypes.NewFullCommitQC(commitQC, []*atypes.BlockHeader{blocks[0].Header()}), blocks)
	appProposal := atypes.NewAppProposal(commitQC.View(), atypes.AppHash{1, 2, 3})
	appQC := atypes.NewAppQC([]*atypes.Signed[*atypes.AppVote]{atypes.Sign(key, atypes.NewAppVote(appProposal))})
	data, err := statesyncproof.Encode(roadBlock, appQC)
	require.NoError(t, err)
	return data
}

func targetAt(psd string) target {
	return target{layout: testLayout(psd), epochGenesis: testEpochGenesis(), chainID: testChainID, mode: tmcfg.ModeFull}
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
	store    *flatkv.CommitStore
	epochRow []byte
	// proofs holds the state sync proof file of each committed height.
	proofs map[int64][]byte
	rows   map[string][]byte
	// rowsAt holds a copy of rows at each committed height.
	rowsAt map[int64]map[string][]byte
}

// newProducer opens a store that keeps keepRecent older checkpoints.
func newProducer(t *testing.T, keepRecent uint32) *producer {
	t.Helper()
	p := &producer{
		target: targetAt(t.TempDir()),
		proofs: map[int64][]byte{},
		rows:   map[string][]byte{},
		rowsAt: map[int64]map[string][]byte{},
	}
	row, err := node.GenesisEpochRegistryRowForTesting(p.epochGenesis, t.TempDir())
	require.NoError(t, err)
	p.epochRow = row
	p.store = openStore(t, p.layout.FlatKVDir, keepRecent)
	t.Cleanup(func() {
		if p.store != nil {
			_ = p.store.Close()
		}
	})
	return p
}

// closeStore closes the producer's store, so the home no longer changes.
func (p *producer) closeStore(t *testing.T) {
	t.Helper()
	require.NoError(t, p.store.Close())
	p.store = nil
}

// commit commits one block that writes to all four data databases, to the bank module and the
// epoch_registry row, waits for its checkpoint to become current, and writes the state sync proof
// file of that checkpoint as a running node does.
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
	epochPairs := []*proto.KVPair{{Key: []byte("epoch_registry"), Value: p.epochRow}}
	require.NoError(t, p.store.ApplyChangeSets(p.store.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: bankPairs}},
		{Name: "autobahn", Changeset: proto.ChangeSet{Pairs: epochPairs}},
	}))
	v, err := p.store.Commit(p.store.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, p.store.FlushSnapshots())
	cur, err := flatkv.ActiveSnapshotVersion(p.layout.FlatKVDir)
	require.NoError(t, err)
	require.Equal(t, v, cur)
	for _, kv := range evmPairs {
		got, ok := p.store.Get("evm", kv.Key)
		require.True(t, ok)
		p.rows["evm/"+string(kv.Key)] = got
	}
	p.rows["bank/"+string(bankPairs[0].Key)] = bankPairs[0].Value
	p.rows["autobahn/epoch_registry"] = p.epochRow
	p.rowsAt[v] = maps.Clone(p.rows)
	p.proofs[v] = stateSyncProofAt(t, v)
	require.NoError(t, os.MkdirAll(p.layout.StateSyncProofDir, 0o750))
	require.NoError(t, os.WriteFile(proofFile(p.layout, v), p.proofs[v], 0o600))
	return v
}

// archiveWithChange packs the producer's checkpoint at height into a file after change edits the
// logical archive entries, and returns its path.
func (p *producer) archiveWithChange(t *testing.T, height int64, change func(m *manifest, entries *[]tarEntry)) string {
	t.Helper()
	stage := t.TempDir()
	staged, err := stageVersion(p.layout, height, stage)
	require.NoError(t, err)
	m, err := buildManifest(p.chainID, height, staged.stats)
	require.NoError(t, err)
	var chunk []byte
	_, err = flatkv.ExportSnapshotDir(t.Context(), staged.checkpoint, height, func(key, value []byte) error {
		chunk = appendRecord(chunk, key, value)
		return nil
	})
	require.NoError(t, err)
	entries := []tarEntry{manifestEntry(t, m), proofEntry(height, staged.proof)}
	if len(chunk) > 0 {
		entries = append(entries, tarEntry{name: rowChunkPath(0), content: chunk})
	}
	if change != nil {
		change(m, &entries)
		for i := range entries {
			if entries[i].name == manifestName {
				entries[i] = manifestEntry(t, m)
				break
			}
		}
	}
	out := filepath.Join(t.TempDir(), "archive.tar.zst")
	require.NoError(t, os.WriteFile(out, buildRawArchive(t, entries), 0o600))
	return out
}

func replaceEntry(entries []tarEntry, name string, content []byte) bool {
	for i := range entries {
		if entries[i].name == name {
			entries[i].content = content
			return true
		}
	}
	return false
}

func appendLogicalRow(entries []tarEntry, key, value []byte) []tarEntry {
	if len(entries) == 0 || entries[len(entries)-1].name != rowChunkPath(0) {
		return append(entries, tarEntry{name: rowChunkPath(0), content: appendRecord(nil, key, value)})
	}
	entries[len(entries)-1].content = appendRecord(entries[len(entries)-1].content, key, value)
	return entries
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

// requireRestored asserts that the home of tg loads at height with the producer's rows, holds the
// epoch registry and the state sync proof file of the producer's checkpoint at height, and holds
// the marker for height.
func requireRestored(t *testing.T, tg target, p *producer, height int64) {
	t.Helper()
	requireMarker(t, tg, utils.Some(height))
	require.NoDirExists(t, filepath.Join(tg.layout.PersistentStateDir, restoreStagingDirName))
	proof, err := os.ReadFile(tg.layout.RestoreProofFile)
	require.NoError(t, err)
	require.Equal(t, p.proofs[height], proof)

	prepared := filepath.Join(t.TempDir(), "prepared")
	checkpoint := filepath.Join(p.layout.FlatKVDir, flatkv.SnapshotDirName(height))
	require.NoError(t, node.PrepareEpochRegistry(p.epochGenesis, checkpoint, height, prepared))
	require.Equal(t, treeOf(t, filepath.Join(prepared, "epochs")), treeOf(t, tg.layout.EpochRegistryDir))

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

// requireMarker asserts that the home of tg holds the restore marker for height, or no marker when
// height is None.
func requireMarker(t *testing.T, tg target, height utils.Option[int64]) {
	t.Helper()
	h, ok := height.Get()
	if !ok {
		require.NoFileExists(t, tg.layout.RestoreMarkerFile)
		return
	}
	marker, err := os.ReadFile(tg.layout.RestoreMarkerFile)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(h, 10)+"\n", string(marker))
}

// flatKVHeight returns the height of the current FlatKV checkpoint of tg, or 0 when FlatKV has none.
func flatKVHeight(t *testing.T, tg target) int64 {
	t.Helper()
	v, err := flatkv.ActiveSnapshotVersion(tg.layout.FlatKVDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	require.NoError(t, err)
	return v
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
