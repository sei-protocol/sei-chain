package upgradetest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

func TestScaffoldCreatesATaggedAppUpgradeTest(t *testing.T) {
	root := t.TempDir()

	path, err := upgradetest.Scaffold(root, "v6.6", "v6.7")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "upgrade_v67_test.go"), path)

	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(source), "//go:build upgrade_v67")
	require.Contains(t, string(source), "package app_test")
	require.Contains(t, string(source), `const v67UpgradeName = "v6.7"`)
	require.Contains(t, string(source), "func newV67Chain")
	require.Contains(t, string(source), "func applyV67")
	require.Contains(t, string(source), "TODO: define the v6.6 -> v6.7 upgrade assertions")
	require.Contains(t, string(source), "func TestV67CrossVersion")
	require.Contains(t, string(source), "upgradetest.RunCrossVersion")
	require.Contains(t, string(source), "TODO: create v6.7 state with the source binary")
	require.Contains(t, string(source), "TODO: verify v6.7 state with the target binary")

	offlineSource, err := os.ReadFile(filepath.Join(
		root, "testdata", "upgrade_v67_offline_source_test.go"))
	require.NoError(t, err)
	require.Contains(t, string(offlineSource),
		"//go:build upgrade_v67 && offline_upgrade && upgrade_source")
	require.Contains(t, string(offlineSource), "func TestV67OfflineUpgradeSource")
	require.Contains(t, string(offlineSource), "TODO: create committed v6.7 source state")
	require.Contains(t, string(offlineSource), "func TestV67OfflineUpgradeReopen")
	require.Contains(t, string(offlineSource), "TODO: reopen the migrated database with the source binary")

	offlineTarget, err := os.ReadFile(filepath.Join(root, "upgrade_v67_offline_target_test.go"))
	require.NoError(t, err)
	require.Contains(t, string(offlineTarget),
		"//go:build upgrade_v67 && offline_upgrade && upgrade_target")
	require.Contains(t, string(offlineTarget), "func TestV67OfflineUpgradeTarget")
	require.Contains(t, string(offlineTarget), "TODO: reopen and verify committed v6.7 target state")

	file, err := upgradetest.ReadTestFile(root, "upgrade_v67_test.go")
	require.NoError(t, err)
	require.Equal(t, "upgrade_v67", file.Tag)
}

func TestScaffoldRefusesToOverwriteASet(t *testing.T) {
	root := t.TempDir()
	path, err := upgradetest.Scaffold(root, "v6.6", "v6.7")
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = upgradetest.Scaffold(root, "v6.6", "v6.7")
	require.ErrorContains(t, err, "already exists")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestScaffoldRefusesToOverwriteAnOfflinePhase(t *testing.T) {
	root := t.TempDir()
	offline := filepath.Join(root, "testdata", "upgrade_v67_offline_source_test.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(offline), 0o750))
	require.NoError(t, os.WriteFile(offline, []byte("existing"), 0o600))

	_, err := upgradetest.Scaffold(root, "v6.6", "v6.7")
	require.ErrorContains(t, err, "already exists")
	_, statErr := os.Stat(filepath.Join(root, "upgrade_v67_test.go"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestScaffoldValidatesBeforeWriting(t *testing.T) {
	root := t.TempDir()

	_, err := upgradetest.Scaffold(root, "v6.6.1", "v6.7")
	require.ErrorContains(t, err, "want vMAJOR.MINOR")

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}
