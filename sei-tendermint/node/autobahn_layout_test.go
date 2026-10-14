package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
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

func TestResolveAutobahnLayout(t *testing.T) {
	conf := layoutConfig(t, filepath.Join("data", "autobahn"))
	layout, err := ResolveAutobahnLayout(conf, gigaconfig.DefaultConfig.Storage)
	require.NoError(t, err)

	psd := filepath.Join(conf.RootDir, "data", "autobahn")
	require.Equal(t, psd, layout.PersistentStateDir)
	require.True(t, utils.OrPanic1(os.Stat(psd)).IsDir())

	want, err := seidbconfig.AutobahnStorageConfig(psd)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(psd, "data", "state_commit", "flatkv"), layout.FlatKVDir)
	require.Equal(t, []string{filepath.Join(psd, "blockdb")}, layout.BlockDBDirs)
	require.Equal(t, want.ReceiptDBConfig.DBDirectory, layout.ReceiptStoreDir)
	require.Equal(t, want.SSConfig.EVMDBDirectory, layout.StateStoreDir)
	require.Equal(t, filepath.Join(psd, "hashvault"), layout.HashVaultDir)
	require.Equal(t, filepath.Join(psd, "state-sync-restored"), layout.RestoreMarkerFile)
}

func TestResolveAutobahnLayoutAbsolutePersistentStateDir(t *testing.T) {
	psd := t.TempDir()
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd), gigaconfig.DefaultConfig.Storage)
	require.NoError(t, err)
	require.Equal(t, psd, layout.PersistentStateDir)
}

func TestResolveAutobahnLayoutLegacyFlatKVDir(t *testing.T) {
	psd := t.TempDir()
	legacy := filepath.Join(psd, "data", "flatkv")
	require.NoError(t, os.MkdirAll(legacy, 0o750))
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd), gigaconfig.DefaultConfig.Storage)
	require.NoError(t, err)
	require.Equal(t, legacy, layout.FlatKVDir)
}

func TestResolveAutobahnLayoutEpochRegistryFiles(t *testing.T) {
	psd := t.TempDir()
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd), gigaconfig.DefaultConfig.Storage)
	require.NoError(t, err)

	committee, _ := atypes.GenCommittee(utils.TestRng(), 4)
	_, err = epoch.NewRegistry(committee, 1, time.Time{}, utils.Some(psd))
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(psd, "epochs"))
	require.NoError(t, err)
	created := make([]string, 0, len(entries))
	for _, e := range entries {
		created = append(created, filepath.Join(psd, "epochs", e.Name()))
	}
	require.Equal(t, created, layout.EpochRegistryFiles)
}

func TestResolveAutobahnLayoutRequiresAutobahnConfig(t *testing.T) {
	_, err := ResolveAutobahnLayout(&config.Config{BaseConfig: config.BaseConfig{RootDir: t.TempDir()}}, gigaconfig.DefaultConfig.Storage)
	require.Error(t, err)
}
