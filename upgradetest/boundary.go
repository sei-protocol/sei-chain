// Package upgradetest selects and scaffolds version-specific upgrade tests.
package upgradetest

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/sei-protocol/sei-chain/app"
)

// A Boundary is an upgrade as an operator performs it: the chain has applied
// From and is about to apply To.
type Boundary struct {
	From string
	To   string
}

// Current returns the latest minor-version boundary this build embeds. Patch
// upgrade names are ignored because they do not define a new minor test.
func Current() (Boundary, error) {
	var names []string
	for _, name := range app.ReleaseUpgrades() {
		if minorVersion.MatchString(name) {
			names = append(names, name)
		}
	}
	if len(names) < 2 {
		return Boundary{}, fmt.Errorf(
			"upgradetest: this build embeds %d minor upgrade names, and a boundary needs two", len(names))
	}
	return NewMinorBoundary(names[len(names)-2], names[len(names)-1])
}

func (b Boundary) String() string {
	return b.From + " -> " + b.To
}

// Tag returns the build tag compiling this boundary's app test file.
func (b Boundary) Tag() (string, error) {
	return TagFor(b.To)
}

// TestFile returns the name of this boundary's app test file.
func (b Boundary) TestFile() (string, error) {
	return TestFileFor(b.To)
}

// OfflineSourceTestFile returns the source-phase Go test for this boundary.
func (b Boundary) OfflineSourceTestFile() (string, error) {
	return OfflineSourceTestFileFor(b.To)
}

// OfflineTargetTestFile returns the target-phase Go test for this boundary.
func (b Boundary) OfflineTargetTestFile() (string, error) {
	return OfflineTargetTestFileFor(b.To)
}

// TagFor returns the build tag compiling the test set for an upgrade name:
// v6.7 gives upgrade_v67, matching app/upgrade_v67_test.go.
func TagFor(upgrade string) (string, error) {
	suffix, err := versionSuffix(upgrade)
	if err != nil {
		return "", err
	}
	return "upgrade_" + suffix, nil
}

// TestFileFor returns the app test file for an upgrade name.
func TestFileFor(upgrade string) (string, error) {
	suffix, err := versionSuffix(upgrade)
	if err != nil {
		return "", err
	}
	return "upgrade_" + suffix + "_test.go", nil
}

// OfflineSourceTestFileFor returns the persisted source-phase app test.
func OfflineSourceTestFileFor(upgrade string) (string, error) {
	suffix, err := versionSuffix(upgrade)
	if err != nil {
		return "", err
	}
	return "upgrade_" + suffix + "_offline_source_test.go", nil
}

// OfflineTargetTestFileFor returns the persisted target-phase app test.
func OfflineTargetTestFileFor(upgrade string) (string, error) {
	suffix, err := versionSuffix(upgrade)
	if err != nil {
		return "", err
	}
	return "upgrade_" + suffix + "_offline_target_test.go", nil
}

// NewMinorBoundary returns a boundary between two minor versions of the same
// major version. The target has to be newer than the source.
func NewMinorBoundary(from, to string) (Boundary, error) {
	fromMajor, fromMinor, err := parseMinorVersion(from)
	if err != nil {
		return Boundary{}, fmt.Errorf("from version: %w", err)
	}
	toMajor, toMinor, err := parseMinorVersion(to)
	if err != nil {
		return Boundary{}, fmt.Errorf("to version: %w", err)
	}
	if fromMajor != toMajor {
		return Boundary{}, fmt.Errorf(
			"minor upgrade %s -> %s crosses major versions", from, to)
	}
	if toMinor <= fromMinor {
		return Boundary{}, fmt.Errorf(
			"minor upgrade target %s must be newer than source %s", to, from)
	}
	return Boundary{From: from, To: to}, nil
}

var minorVersion = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

func parseMinorVersion(version string) (uint64, uint64, error) {
	parts := minorVersion.FindStringSubmatch(version)
	if parts == nil {
		return 0, 0, fmt.Errorf(
			"%q is not a minor version (want vMAJOR.MINOR)", version)
	}
	major, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("major version in %q: %w", version, err)
	}
	minor, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("minor version in %q: %w", version, err)
	}
	return major, minor, nil
}

func versionSuffix(version string) (string, error) {
	parts := minorVersion.FindStringSubmatch(version)
	if parts == nil {
		return "", fmt.Errorf(
			"upgradetest: %q is not a minor version (want vMAJOR.MINOR)", version)
	}
	return "v" + parts[1] + parts[2], nil
}
