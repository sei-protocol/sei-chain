package upgradetest_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

func TestTagAndFileSpelling(t *testing.T) {
	for _, tc := range []struct {
		upgrade string
		tag     string
		file    string
	}{
		{upgrade: "v6.6", tag: "upgrade_v66", file: "upgrade_v66_test.go"},
		{upgrade: "v6.7", tag: "upgrade_v67", file: "upgrade_v67_test.go"},
		{upgrade: "v7.0", tag: "upgrade_v70", file: "upgrade_v70_test.go"},
	} {
		t.Run(tc.upgrade, func(t *testing.T) {
			tag, err := upgradetest.TagFor(tc.upgrade)
			require.NoError(t, err)
			require.Equal(t, tc.tag, tag)

			file, err := upgradetest.TestFileFor(tc.upgrade)
			require.NoError(t, err)
			require.Equal(t, tc.file, file)
		})
	}
}

func TestTagForRequiresAMinorVersion(t *testing.T) {
	for _, upgrade := range []string{
		"", "6.7", "v6", "v6.7.1", "1.0.4beta", "v4.0.0-evm-devnet",
	} {
		_, err := upgradetest.TagFor(upgrade)
		require.Error(t, err, "TagFor(%q) should be refused", upgrade)
	}
}

func TestNewMinorBoundaryRejectsInvalidBumps(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		err  string
	}{
		{name: "source patch", from: "v6.6.3", to: "v6.7", err: "want vMAJOR.MINOR"},
		{name: "target patch", from: "v6.6", to: "v6.7.1", err: "want vMAJOR.MINOR"},
		{name: "major bump", from: "v6.7", to: "v7.0", err: "crosses major versions"},
		{name: "same version", from: "v6.7", to: "v6.7", err: "must be newer"},
		{name: "backwards", from: "v6.7", to: "v6.6", err: "must be newer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := upgradetest.NewMinorBoundary(tc.from, tc.to)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestCurrentIsTheLastTwoEmbeddedMinorUpgrades(t *testing.T) {
	var names []string
	for _, name := range app.ReleaseUpgrades() {
		if _, err := upgradetest.TagFor(name); err == nil {
			names = append(names, name)
		}
	}
	require.GreaterOrEqual(t, len(names), 2)

	boundary, err := upgradetest.Current()
	require.NoError(t, err)
	require.Equal(t, names[len(names)-2], boundary.From)
	require.Equal(t, names[len(names)-1], boundary.To)
}

// Minor names lose their dots in file and tag names. Keep that shortening
// one-to-one across the embedded release list.
func TestEveryMinorUpgradeHasItsOwnTag(t *testing.T) {
	spelledBy := map[string]string{}
	for _, upgrade := range app.ReleaseUpgrades() {
		tag, err := upgradetest.TagFor(upgrade)
		if err != nil {
			continue
		}
		require.NotContains(t, spelledBy, tag,
			"%s and %s both spell %s", spelledBy[tag], upgrade, tag)
		spelledBy[tag] = upgrade
	}
	require.NotEmpty(t, spelledBy)
}
