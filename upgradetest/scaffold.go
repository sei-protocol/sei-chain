package upgradetest

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

// Scaffold creates a version-specific upgrade test in the app package. Root is
// the app directory; the returned path is the new Go test file.
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
	suffix, err := versionSuffix(to)
	if err != nil {
		return "", err
	}
	exportedSuffix := strings.ToUpper(suffix[:1]) + suffix[1:]

	testPath := filepath.Join(root, fileName)
	if _, err := os.Stat(testPath); err == nil {
		return "", fmt.Errorf("upgrade test %s already exists", testPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect upgrade test %s: %w", testPath, err)
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
	source, err = format.Source(source)
	if err != nil {
		return "", fmt.Errorf("format generated upgrade test: %w", err)
	}
	if err := os.WriteFile(testPath, source, 0o644); err != nil { //nolint:gosec // generated Go source uses repository file permissions
		_ = os.Remove(testPath)
		return "", fmt.Errorf("write upgrade test %s: %w", testPath, err)
	}
	return testPath, nil
}
