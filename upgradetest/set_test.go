package upgradetest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

const appDir = "../app"

// Appending a minor version to app/tags without defining its version-specific
// test fails here. The ordinary Go test run cannot see a tagged test file.
func TestCurrentBoundaryHasATestFile(t *testing.T) {
	boundary, err := upgradetest.Current()
	require.NoError(t, err)
	fileName, err := boundary.TestFile()
	require.NoError(t, err)
	tag, err := boundary.Tag()
	require.NoError(t, err)

	file, err := upgradetest.ReadTestFile(appDir, fileName)
	require.NoError(t, err,
		"the %s boundary has no app/%s; create it with make new-upgrade-test FROM=%s TO=%s",
		boundary, fileName, boundary.From, boundary.To)
	require.Equal(t, tag, file.Tag,
		"app/%s has build tag %q; want %q", fileName, file.Tag, tag)
}

func TestCurrentBoundaryHasOfflinePhaseFiles(t *testing.T) {
	boundary, err := upgradetest.Current()
	require.NoError(t, err)
	source, err := boundary.OfflineSourceTestFile()
	require.NoError(t, err)
	target, err := boundary.OfflineTargetTestFile()
	require.NoError(t, err)

	for _, file := range []string{filepath.Join("testdata", source), target} {
		_, err := os.Stat(filepath.Join(appDir, file))
		require.NoError(t, err,
			"the %s boundary has no app/%s; create it with make new-upgrade-test FROM=%s TO=%s",
			boundary, filepath.ToSlash(file), boundary.From, boundary.To)
	}
}

// Each version-specific file carries the tag implied by its existing app file
// name. The generic app/upgrade_test.go is deliberately outside this check.
func TestVersionSpecificUpgradeTestsCarryTheirOwnTag(t *testing.T) {
	files, err := upgradetest.TestFiles(appDir)
	require.NoError(t, err)
	require.NotEmpty(t, files, "no version-specific app upgrade tests found")

	for _, file := range files {
		t.Run(file.Name, func(t *testing.T) {
			require.Equal(t, file.ExpectedTag(), file.Tag,
				"app/%s is constrained by %s; want %s",
				file.Name, describeTag(file.Tag), file.ExpectedTag())
		})
	}
}

func describeTag(tag string) string {
	if tag == "" {
		return "no single build tag"
	}
	return tag
}
