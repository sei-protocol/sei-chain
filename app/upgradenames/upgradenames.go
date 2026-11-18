// Package upgradenames holds the upgrade names this build ships.
package upgradenames

import (
	_ "embed"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

//go:embed tags
var tags string

var release = Parse(tags)

// Release returns the upgrade names this build embeds, in semver order.
func Release() []string {
	return slices.Clone(release)
}

// Latest returns the last name of Release.
func Latest() string {
	return release[len(release)-1]
}

// Parse splits a newline- or comma-separated list of upgrade names and returns
// them in semver order.
func Parse(list string) []string {
	upgrades := strings.FieldsFunc(list, func(r rune) bool {
		return r == '\n' || r == ','
	})
	// Upgrades names must be in alphabetical order
	// https://github.com/cosmos/cosmos-sdk/issues/11707
	semver.Sort(upgrades)
	return upgrades
}
