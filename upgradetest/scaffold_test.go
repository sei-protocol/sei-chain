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

	path, err := upgradetest.Scaffold(root, "v6.7", "v6.8")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "upgrade_v68_test.go"), path)

	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(source), "//go:build upgrade_v68")
	require.Contains(t, string(source), "package app_test")
	require.Contains(t, string(source), `const v68UpgradeName = "v6.8"`)
	require.Contains(t, string(source), "func newV68Chain")
	require.Contains(t, string(source), "func applyV68")
	require.Contains(t, string(source), "TODO: define the v6.7 -> v6.8 upgrade assertions")
	require.Contains(t, string(source), "func TestV68CrossVersion")
	require.Contains(t, string(source), "upgradetest.RunCrossVersion")
	require.Contains(t, string(source), "TODO: create v6.8 state with the source binary")
	require.Contains(t, string(source), "TODO: verify v6.8 state with the target binary")

	file, err := upgradetest.ReadTestFile(root, "upgrade_v68_test.go")
	require.NoError(t, err)
	require.Equal(t, "upgrade_v68", file.Tag)
}

func TestScaffoldRefusesToOverwriteASet(t *testing.T) {
	root := t.TempDir()
	path, err := upgradetest.Scaffold(root, "v6.7", "v6.8")
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = upgradetest.Scaffold(root, "v6.7", "v6.8")
	require.ErrorContains(t, err, "already exists")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestScaffoldValidatesBeforeWriting(t *testing.T) {
	root := t.TempDir()

	_, err := upgradetest.Scaffold(root, "v6.7.1", "v6.8")
	require.ErrorContains(t, err, "want vMAJOR.MINOR")

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}
