package statesyncsnapshot

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

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
	require.ElementsMatch(t, []string{"registry_a.pb", "registry_b.pb"}, m.epochFileNames())
	for _, f := range m.Files {
		require.NotEqual(t, "LOCK", path.Base(f.Path))
	}
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

func TestCreateRefusesARegistryNewerThanTheCheckpoint(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)

	now := time.Now()
	require.NoError(t, os.Chtimes(p.layout.EpochRegistryFiles[1], now, now))
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{}),
		"a 0-byte registry file never refuses")

	require.NoError(t, os.Chtimes(p.layout.EpochRegistryFiles[0], now.Add(2*registryMargin), now.Add(2*registryMargin)))
	err := runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.ErrorContains(t, err, "the epoch registry changed after checkpoint")
	require.NoDirExists(t, filepath.Join(p.layout.PersistentStateDir, createStagingDirName))
}

func TestCreateFailsWhileAnotherCreateRuns(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)

	unlock, err := lockCreate(p.layout.PersistentStateDir)
	require.NoError(t, err)
	err = runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.ErrorContains(t, err, "another state-sync-snapshot create is running")
	unlock()
	require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &bytes.Buffer{}, &bytes.Buffer{}))
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
