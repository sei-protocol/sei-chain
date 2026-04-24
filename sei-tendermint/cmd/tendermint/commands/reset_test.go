package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	cfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func Test_ResetAll(t *testing.T) {
	config := cfg.TestConfig()
	dir := t.TempDir()
	config.SetRoot(dir)

	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetAll(config.DBDir(), config.PrivValidator.KeyFile(),
		config.PrivValidator.StateFile(), types.ABCIPubKeyTypeEd25519, ""))
	require.DirExists(t, config.DBDir())
	require.NoFileExists(t, filepath.Join(config.DBDir(), "block.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "state.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "evidence.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "tx_index.db"))
	require.FileExists(t, config.PrivValidator.StateFile())
	pv, err = privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	require.Equal(t, int64(0), pv.LastSignState.Height)
}

func Test_ResetState(t *testing.T) {
	config := cfg.TestConfig()
	dir := t.TempDir()
	config.SetRoot(dir)

	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetState(config.DBDir()))
	require.DirExists(t, config.DBDir())
	require.NoFileExists(t, filepath.Join(config.DBDir(), "block.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "state.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "evidence.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "tx_index.db"))
	require.FileExists(t, config.PrivValidator.StateFile())
	pv, err = privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	// private validator state should still be in tact.
	require.Equal(t, int64(10), pv.LastSignState.Height)
}

func Test_UnsafeResetAll(t *testing.T) {
	config := cfg.TestConfig()
	dir := t.TempDir()
	config.SetRoot(dir)

	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetAll(config.DBDir(), config.PrivValidator.KeyFile(),
		config.PrivValidator.StateFile(), types.ABCIPubKeyTypeEd25519, ""))
	require.DirExists(t, config.DBDir())
	require.NoFileExists(t, filepath.Join(config.DBDir(), "block.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "state.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "evidence.db"))
	require.NoFileExists(t, filepath.Join(config.DBDir(), "tx_index.db"))
	require.FileExists(t, config.PrivValidator.StateFile())
	pv, err = privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	require.Equal(t, int64(0), pv.LastSignState.Height)
}

func initTestFiles(t *testing.T, config *cfg.Config) {
	t.Helper()

	privValKeyFile := config.PrivValidator.KeyFile()
	privValStateFile := config.PrivValidator.StateFile()

	require.NoError(t, os.MkdirAll(filepath.Dir(privValKeyFile), 0o755))

	pv, err := privval.GenFilePV(privValKeyFile, privValStateFile, types.ABCIPubKeyTypeEd25519)
	require.NoError(t, err)
	require.NoError(t, pv.Save())
}

// createDirs is a test helper that creates the given directories under base.
func createDirs(t *testing.T, base string, dirs []string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(base, d), 0o755))
	}
}

// legacyDBs are the flat data/ DB directories that ResetState should remove.
var legacyDBs = []string{
	"blockstore.db", "state.db", "cs.wal", "evidence.db", "tx_index.db",
}

// newTendermintDBs are the data/tendermint/ DB directories that ResetState should remove.
var newTendermintDBs = []string{
	"tendermint/blockstore.db", "tendermint/state.db", "tendermint/cs.wal",
	"tendermint/evidence.db", "tendermint/tx_index.db", "tendermint/peerstore.db",
}

func TestResetState_LegacyLayout(t *testing.T) {
	dbDir := t.TempDir()
	createDirs(t, dbDir, legacyDBs)

	for _, d := range legacyDBs {
		require.DirExists(t, filepath.Join(dbDir, d))
	}

	require.NoError(t, ResetState(dbDir))

	for _, d := range legacyDBs {
		require.NoDirExists(t, filepath.Join(dbDir, d))
	}
	require.DirExists(t, dbDir)
}

func TestResetState_NewLayout(t *testing.T) {
	dbDir := t.TempDir()
	createDirs(t, dbDir, newTendermintDBs)

	for _, d := range newTendermintDBs {
		require.DirExists(t, filepath.Join(dbDir, d))
	}

	require.NoError(t, ResetState(dbDir))

	for _, d := range newTendermintDBs {
		require.NoDirExists(t, filepath.Join(dbDir, d))
	}
	require.DirExists(t, dbDir)
}

func TestResetState_BothLayouts(t *testing.T) {
	dbDir := t.TempDir()
	all := append(legacyDBs, newTendermintDBs...)
	createDirs(t, dbDir, all)

	require.NoError(t, ResetState(dbDir))

	for _, d := range all {
		require.NoDirExists(t, filepath.Join(dbDir, d))
	}
	require.DirExists(t, dbDir)
}

func TestResetState_EmptyDir(t *testing.T) {
	dbDir := t.TempDir()
	require.NoError(t, ResetState(dbDir))
	require.DirExists(t, dbDir)
}

func TestResetPeerStore_LegacyOnly(t *testing.T) {
	dbDir := t.TempDir()
	legacy := filepath.Join(dbDir, "peerstore.db")
	require.NoError(t, os.MkdirAll(legacy, 0o755))

	require.NoError(t, ResetPeerStore(dbDir))
	require.NoDirExists(t, legacy)
}

func TestResetPeerStore_NewOnly(t *testing.T) {
	dbDir := t.TempDir()
	newPath := filepath.Join(dbDir, "tendermint", "peerstore.db")
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	require.NoError(t, ResetPeerStore(dbDir))
	require.NoDirExists(t, newPath)
}

func TestResetPeerStore_BothLayouts(t *testing.T) {
	dbDir := t.TempDir()
	legacy := filepath.Join(dbDir, "peerstore.db")
	newPath := filepath.Join(dbDir, "tendermint", "peerstore.db")
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	require.NoError(t, ResetPeerStore(dbDir))
	require.NoDirExists(t, legacy)
	require.NoDirExists(t, newPath)
}

func TestResetPeerStore_NeitherExists(t *testing.T) {
	dbDir := t.TempDir()
	require.NoError(t, ResetPeerStore(dbDir))
}
