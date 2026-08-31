package upgradetest

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

// Scaffold creates the in-process, offline, and live definitions for one
// version-specific app upgrade test. It returns the main tagged test path.
func Scaffold(root, from, to string) (string, error) {
	boundary, err := NewMinorBoundary(from, to)
	if err != nil {
		return "", err
	}
	tag, err := boundary.Tag()
	if err != nil {
		return "", err
	}
	fileName, err := boundary.TestFile()
	if err != nil {
		return "", err
	}
	offlineSourceName, err := boundary.OfflineSourceTestFile()
	if err != nil {
		return "", err
	}
	offlineTargetName, err := boundary.OfflineTargetTestFile()
	if err != nil {
		return "", err
	}
	suffix, err := versionSuffix(to)
	if err != nil {
		return "", err
	}
	exportedSuffix := strings.ToUpper(suffix[:1]) + suffix[1:]

	testPath := filepath.Join(root, fileName)
	paths := []string{
		testPath,
		filepath.Join(root, offlineSourceName),
		filepath.Join(root, offlineTargetName),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("upgrade test %s already exists", path)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect upgrade test %s: %w", path, err)
		}
	}

	source := []byte(fmt.Sprintf(`//go:build %[1]s

package app_test

import (
	"testing"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
)

const %[2]sUpgradeName = %[3]q

func new%[4]sChain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", %[2]sUpgradeName)
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	return app
}

func apply%[4]s(t *testing.T, app *processblock.App) {
	t.Helper()
	app.UpgradeKeeper.ApplyUpgrade(app.Ctx(), upgradetypes.Plan{
		Name:   %[2]sUpgradeName,
		Height: app.Ctx().BlockHeight(),
	})
}

func Test%[4]sUpgrade(t *testing.T) {
	app := new%[4]sChain(t)
	apply%[4]s(t, app)
	t.Fatal("TODO: define the %[5]s upgrade assertions")
}

func Test%[4]sCrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t,
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			t.Fatal("TODO: create %[3]s state with the source binary")
		},
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			t.Fatal("TODO: verify %[3]s state with the target binary")
		},
	)
}
`, tag, suffix, to, exportedSuffix, boundary))
	offlineSource := []byte(fmt.Sprintf(`//go:build %[1]s && offline_upgrade && upgrade_source

package app

import "testing"

func Test%[2]sOfflineUpgradeSource(t *testing.T) {
	_ = requireOfflineUpgradePhase(t, "source")
	t.Fatal("TODO: create committed %[3]s source state")
}
`, tag, exportedSuffix, to))
	offlineTarget := []byte(fmt.Sprintf(`//go:build %[1]s && offline_upgrade && upgrade_target

package app

import "testing"

func Test%[2]sOfflineUpgradeTarget(t *testing.T) {
	_ = requireOfflineUpgradePhase(t, "target")
	t.Fatal("TODO: reopen and verify committed %[3]s target state")
}
`, tag, exportedSuffix, to))

	sources := [][]byte{source, offlineSource, offlineTarget}
	for i := range sources {
		sources[i], err = format.Source(sources[i])
		if err != nil {
			return "", fmt.Errorf("format generated upgrade test %s: %w", paths[i], err)
		}
	}
	for i, path := range paths {
		if err := os.WriteFile(path, sources[i], 0o644); err != nil { //nolint:gosec // generated Go source uses repository file permissions
			for _, written := range paths[:i+1] {
				_ = os.Remove(written)
			}
			return "", fmt.Errorf("write upgrade test %s: %w", path, err)
		}
	}
	return testPath, nil
}
