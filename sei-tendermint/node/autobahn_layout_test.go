package node

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// layoutConfig returns a node config rooted at a new home whose Autobahn config sets
// persistent_state_dir to psd.
func layoutConfig(t *testing.T, psd string) *config.Config {
	t.Helper()
	validator := makeValidator([]byte("layout-validator"), []byte("layout-node"), "localhost:26660")
	fc := defaultFileConfig(t, []config.AutobahnValidator{validator})
	fc.PersistentStateDir = psd
	return &config.Config{
		BaseConfig:         config.BaseConfig{RootDir: t.TempDir(), Mode: config.ModeFull},
		AutobahnConfigFile: writeAutobahnConfig(t, fc),
	}
}

// TestResolveAutobahnLayout holds the layout against the paths node startup opens for a
// relative persistent_state_dir.
func TestResolveAutobahnLayout(t *testing.T) {
	conf := layoutConfig(t, filepath.Join("data", "autobahn"))
	layout, err := ResolveAutobahnLayout(conf, gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.NoError(t, err)

	psd := filepath.Join(conf.RootDir, "data", "autobahn")
	require.Equal(t, psd, layout.PersistentStateDir)
	require.True(t, utils.OrPanic1(os.Stat(psd)).IsDir())

	// The FlatKV dir is the one the storage manager opens at startup.
	want, err := seidbconfig.AutobahnStorageConfig(psd)
	require.NoError(t, err)
	require.Equal(t, want.FlatKVConfig.DataDir, layout.FlatKVDir)
	require.Equal(t, filepath.Join(psd, "data", "state_commit", "flatkv"), layout.FlatKVDir)
	require.Equal(t, []string{filepath.Join(psd, "blockdb")}, layout.BlockDBDirs)
	require.Equal(t, want.ReceiptDBConfig.DBDirectory, layout.ReceiptStoreDir)
	require.Equal(t, want.SSConfig.EVMDBDirectory, layout.StateStoreDir)
	require.Equal(t, want.AppHashBuilderConfig.HashVaultConfig.Path, layout.HashVaultDir)
	require.Equal(t, filepath.Join(psd, "state-sync-restored"), layout.RestoreMarkerFile)
	require.Equal(t, filepath.Join(psd, "data", "state_commit", "state_sync_proofs"), layout.StateSyncProofDir)
}

func TestResolveAutobahnLayoutAbsolutePersistentStateDir(t *testing.T) {
	psd := t.TempDir()
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd), gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.NoError(t, err)
	require.Equal(t, psd, layout.PersistentStateDir)
}

func TestResolveAutobahnLayoutLegacyFlatKVDir(t *testing.T) {
	psd := t.TempDir()
	legacy := filepath.Join(psd, "data", "flatkv")
	require.NoError(t, os.MkdirAll(legacy, 0o750))
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd), gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.NoError(t, err)
	require.Equal(t, legacy, layout.FlatKVDir)
}

func TestResolveAutobahnLayoutRequiresAutobahnConfig(t *testing.T) {
	_, err := ResolveAutobahnLayout(&config.Config{BaseConfig: config.BaseConfig{RootDir: t.TempDir()}}, gigaconfig.DefaultConfig.Storage, genesisAt(1))
	require.Error(t, err)
}

// TestAutobahnLayoutCoversWhatAFullnodeStoresOpen requires every entry that a fullnode's storage
// manager creates under the persistent state dir to be a layout path, inside one, or a parent of
// one. A new store under that dir fails here until state sync decides whether restore must require
// it empty or create must pack it.
func TestAutobahnLayoutCoversWhatAFullnodeStoresOpen(t *testing.T) {
	conf := layoutConfig(t, filepath.Join("data", "autobahn"))
	storage := gigaconfig.DefaultConfig.Storage
	storage.Receipts = true
	layout, err := ResolveAutobahnLayout(conf, storage, genesisAt(1))
	require.NoError(t, err)

	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	require.NoError(t, err)
	manager, err := openAutobahnStorageManager(t.Context(), conf.RootDir, conf.Mode, fc, storage, genesisAt(1))
	require.NoError(t, err)
	require.NoError(t, manager.Close())

	storeDirs := append([]string{layout.FlatKVDir, layout.ReceiptStoreDir, layout.StateStoreDir}, layout.BlockDBDirs...)
	for _, dir := range storeDirs {
		info, err := os.Stat(dir)
		require.NoError(t, err, "the storage manager did not create %s", dir)
		require.True(t, info.IsDir())
	}
	dirs := append(storeDirs, layout.HashVaultDir, layout.StateSyncProofDir)
	files := []string{layout.RestoreMarkerFile}
	isParent := func(path string) bool {
		for _, p := range append(slices.Clone(dirs), files...) {
			if strings.HasPrefix(p, path+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}

	var uncovered []string
	err = filepath.WalkDir(layout.PersistentStateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case path == layout.PersistentStateDir || slices.Contains(files, path):
			return nil
		case slices.Contains(dirs, path):
			return fs.SkipDir
		case d.IsDir() && isParent(path):
			return nil
		}
		uncovered = append(uncovered, path)
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, uncovered, "add these paths to AutobahnLayout, or state here why state sync ignores them")
}
