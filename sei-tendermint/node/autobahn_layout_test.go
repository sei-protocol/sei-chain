package node

import (
	"os"
	"path/filepath"
	"testing"

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
	layout, err := ResolveAutobahnLayout(conf)
	require.NoError(t, err)

	psd := filepath.Join(conf.RootDir, "data", "autobahn")
	require.Equal(t, psd, layout.PersistentStateDir)
	require.True(t, utils.OrPanic1(os.Stat(psd)).IsDir())

	// The FlatKV dir is the one the storage manager opens at startup.
	want, err := seidbconfig.AutobahnStorageConfig(psd)
	require.NoError(t, err)
	require.Equal(t, want.FlatKVConfig.DataDir, layout.FlatKVDir)
	require.Equal(t, filepath.Join(psd, "data", "state_commit", "flatkv"), layout.FlatKVDir)
	require.Equal(t, filepath.Join(psd, "state-sync-restored"), layout.RestoreMarkerFile)
}

func TestResolveAutobahnLayoutAbsolutePersistentStateDir(t *testing.T) {
	psd := t.TempDir()
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd))
	require.NoError(t, err)
	require.Equal(t, psd, layout.PersistentStateDir)
}

func TestResolveAutobahnLayoutLegacyFlatKVDir(t *testing.T) {
	psd := t.TempDir()
	legacy := filepath.Join(psd, "data", "flatkv")
	require.NoError(t, os.MkdirAll(legacy, 0o750))
	layout, err := ResolveAutobahnLayout(layoutConfig(t, psd))
	require.NoError(t, err)
	require.Equal(t, legacy, layout.FlatKVDir)
}

func TestResolveAutobahnLayoutRequiresAutobahnConfig(t *testing.T) {
	_, err := ResolveAutobahnLayout(&config.Config{BaseConfig: config.BaseConfig{RootDir: t.TempDir()}})
	require.Error(t, err)
}
