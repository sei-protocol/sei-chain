package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	cfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func Test_ResetAll(t *testing.T) {
	config := cfg.TestConfig()
	dir := t.TempDir()
	config.SetRoot(dir)
	logger := log.NewNopLogger()
	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetAll(config.DBDir(), config.PrivValidator.KeyFile(),
		config.PrivValidator.StateFile(), logger, types.ABCIPubKeyTypeEd25519, ""))
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
	logger := log.NewNopLogger()
	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetState(config.DBDir(), logger))
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
	logger := log.NewNopLogger()
	cfg.EnsureRoot(dir)
	initTestFiles(t, config)
	pv, err := privval.LoadFilePV(config.PrivValidator.KeyFile(), config.PrivValidator.StateFile())
	require.NoError(t, err)
	pv.LastSignState.Height = 10
	require.NoError(t, pv.Save())
	require.NoError(t, ResetAll(config.DBDir(), config.PrivValidator.KeyFile(),
		config.PrivValidator.StateFile(), logger, types.ABCIPubKeyTypeEd25519, ""))
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
