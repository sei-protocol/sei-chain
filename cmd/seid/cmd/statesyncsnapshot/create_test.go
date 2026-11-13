package statesyncsnapshot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

func readManifest(t *testing.T, archive string) *manifest {
	t.Helper()
	f, err := os.Open(archive) //nolint:gosec // test path.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	a, m, err := openArchive(t.Context(), f)
	require.NoError(t, err)
	a.Close()
	return m
}

func TestCreatePacksTheCurrentCheckpointAndLeavesNoStagingFiles(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	h := p.commit(t, 2)
	p.closeStore(t)

	stagingRoot := filepath.Join(p.layout.PersistentStateDir, createStagingDirName)
	require.NoError(t, os.MkdirAll(filepath.Join(stagingRoot, "create-old"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stagingRoot, "create-old", "x"), []byte("x"), 0o600))
	out := filepath.Join(t.TempDir(), "archive.tar.zst")
	require.NoError(t, os.WriteFile(out+".partial", []byte("stale"), 0o600))

	var stderr bytes.Buffer
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: out}, nil, &stderr))
	require.Equal(t, h, heightLine(t, stderr.String()))
	require.NoDirExists(t, stagingRoot)
	require.NoFileExists(t, out+".partial")

	m := readManifest(t, out)
	require.Equal(t, h, m.CheckpointHeight)
	require.Equal(t, testChainID, m.ChainID)
	require.Positive(t, m.RowCount)
	require.Positive(t, m.StateBytes)

	f, err := os.Open(out) //nolint:gosec // test path.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	a, m, err := openArchive(t.Context(), f)
	require.NoError(t, err)
	defer a.Close()
	proof, err := a.readProof(m.CheckpointHeight)
	require.NoError(t, err)
	require.Equal(t, p.proofs[h], proof)
}

func TestCreatePacksTheNewestCheckpointWithAStateSyncProofFile(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	h := p.commit(t, 2)
	require.NoError(t, os.Remove(proofFile(p.layout, p.commit(t, 3))))

	archive, got := p.createArchive(t)
	require.Equal(t, h, got)
	dst := freshHome(t)
	require.NoError(t, runRestore(t.Context(), dst, archive, nil, &bytes.Buffer{}))
	requireRestored(t, dst, p, h)
}

func TestCreateWithoutAStateSyncProofFileFails(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)
	require.NoError(t, os.Remove(proofFile(p.layout, h)))

	for _, height := range []int64{0, h} {
		var stdout bytes.Buffer
		err := runCreate(t.Context(), p.target, createOptions{out: "-", height: height}, &stdout, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoProofFile)
		require.Zero(t, stdout.Len())
	}
}

func TestCreateRefusesAStateSyncProofFileOfAnotherHeight(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)
	require.NoError(t, os.WriteFile(proofFile(p.layout, h), stateSyncProofAt(t, h+1), 0o600))

	var stdout bytes.Buffer
	require.Error(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &stdout, &bytes.Buffer{}))
	require.Zero(t, stdout.Len())
}

func TestCreateToStdoutPrintsOneHeightLine(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)

	var archive, stderr bytes.Buffer
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &archive, &stderr))
	require.Equal(t, h, heightLine(t, stderr.String()))
	a, m, err := openArchive(t.Context(), &archive)
	require.NoError(t, err)
	a.Close()
	require.Equal(t, h, m.CheckpointHeight)
}

func TestCreateFailsWhileAnotherCreateRuns(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)

	unlock, err := lockCreate(p.layout.PersistentStateDir)
	require.NoError(t, err)
	err = runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.ErrorIs(t, err, errCreateRunning)
	unlock()
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{}))
}

func TestCreateFailsWhileAnotherCreateWritesToTheSameDir(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	outDir := t.TempDir()
	out := filepath.Join(outDir, "archive.tar.zst")
	partial := out + ".partial"
	require.NoError(t, os.WriteFile(partial, []byte("another create"), 0o600))

	unlock, err := lockCreate(outDir)
	require.NoError(t, err)
	opts := createOptions{out: out, stagingParent: t.TempDir()}
	err = runCreate(t.Context(), p.target, opts, nil, &bytes.Buffer{})
	require.ErrorIs(t, err, errCreateRunning)
	require.FileExists(t, partial)
	unlock()
	require.NoError(t, runCreate(t.Context(), p.target, opts, nil, &bytes.Buffer{}))
	require.NoFileExists(t, partial)
}

func TestCreateWritesIntoItsStagingParent(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)
	out := filepath.Join(p.layout.PersistentStateDir, "archive.tar.zst")

	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: out}, nil, &bytes.Buffer{}))
	require.Equal(t, h, readManifest(t, out).CheckpointHeight)
}

func TestCreateLinksSurvivePruningWhileItPacks(t *testing.T) {
	p := newProducer(t, 0)
	h := p.commit(t, 1)
	out := filepath.Join(t.TempDir(), "archive.tar.zst")

	pruneBeforeWrite := func(step string) error {
		if step == createStepManifest {
			p.commit(t, 2)
			p.commit(t, 3)
			require.NoDirExists(t, filepath.Join(p.layout.FlatKVDir, flatkv.SnapshotDirName(h)))
		}
		return nil
	}
	require.NoError(t, createWith(t.Context(), p.target, createOptions{out: out}, nil, &bytes.Buffer{}, pruneBeforeWrite))

	dst := freshHome(t)
	require.NoError(t, runRestore(t.Context(), dst, out, nil, &bytes.Buffer{}))
	s := openStore(t, dst.layout.FlatKVDir, 2)
	defer func() { require.NoError(t, s.Close()) }()
	require.Equal(t, h, s.Version())
}

func TestCreateWithAPrunedHeightFails(t *testing.T) {
	p := newProducer(t, 0)
	h := p.commit(t, 1)
	p.commit(t, 2)

	err := runCreate(t.Context(), p.target, createOptions{out: "-", height: h}, &bytes.Buffer{}, &bytes.Buffer{})
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestResolveTargetRefusesBeforeItWritesAnyFile(t *testing.T) {
	home := t.TempDir()
	cfg := tmcfg.TestConfig().SetRoot(home)

	cfg.AutobahnConfigFile = ""
	_, err := resolveTargetFromConfig(cfg, appOptions{gigaconfig.FlagStateSyncEnabled: true})
	require.ErrorContains(t, err, "autobahn-config-file is not set")

	cfg.AutobahnConfigFile = "config/autobahn.json"
	_, err = resolveTargetFromConfig(cfg, appOptions{})
	require.ErrorIs(t, err, errDisabled)
	_, err = resolveTargetFromConfig(cfg, appOptions{gigaconfig.FlagStateSyncEnabled: false})
	require.ErrorIs(t, err, errDisabled)

	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries)
}

type appOptions map[string]any

func (o appOptions) Get(key string) any { return o[key] }
