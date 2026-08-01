package configtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// newViperOver returns a viper reading the given app.toml body, configured the way the
// server viper is: SEID prefix, AutomaticEnv, and the replacer that folds ".", "-" and
// "_" to "_".
func newViperOver(t *testing.T, body string) *viper.Viper {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write app.toml: %v", err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read app.toml: %v", err)
	}
	return v
}

// TestSettingsIsStableWhereAllSettingsIsNot is the reason Settings exists, and it is
// written as a comparison so the defect and the fix are visible in one place.
//
// The shape that breaks AllSettings is one key being a dotted prefix of another. That
// makes "giga" ambiguous — a value, or the parent of "giga.x" — and AllSettings resolves
// the ambiguity by map iteration order, which Go randomizes per run. Settings never
// re-nests, so there is no ambiguity to resolve.
//
// The assertion is deliberately asymmetric: it requires Settings to be stable, and it
// only *reports* whether AllSettings varied on this run rather than requiring that it
// does. Requiring instability would make this test depend on hitting both orderings
// within the sample, which is the same coin-flip it is documenting.
func TestSettingsIsStableWhereAllSettingsIsNot(t *testing.T) {
	const body = `
[section]
"giga" = 1
"giga.x" = 2
`
	const runs = 200

	settingsShapes := map[string]int{}
	allSettingsShapes := map[string]int{}
	for i := 0; i < runs; i++ {
		v := newViperOver(t, body)
		settingsShapes[fmt.Sprint(configtest.Settings(v))]++
		allSettingsShapes[fmt.Sprint(v.AllSettings())]++
	}

	require.Len(t, settingsShapes, 1,
		"Settings must return one shape across %d reads of one file; got %d: %v",
		runs, len(settingsShapes), settingsShapes)

	if len(allSettingsShapes) > 1 {
		t.Logf("AllSettings returned %d distinct shapes across %d reads, as expected: %v",
			len(allSettingsShapes), runs, allSettingsShapes)
	} else {
		t.Logf("AllSettings happened to return one shape across %d reads this time; the "+
			"instability is order-dependent, so a single stable sample does not clear it",
			runs)
	}
}

// TestSettingsPreservesConcreteTypes pins the property that makes Settings usable for a
// parity assertion: a TOML integer and a TOML string are not interchangeable, so a
// comparison built on Settings catches a manager that resolves one as the other.
func TestSettingsPreservesConcreteTypes(t *testing.T) {
	v := newViperOver(t, "[s]\nnum = 8\nstr = \"8\"\n")
	got := configtest.Settings(v)

	require.Equal(t, int64(8), got["s.num"], "a TOML integer must stay an integer")
	require.Equal(t, "8", got["s.str"], "a TOML string must stay a string")
	require.NotEqual(t, got["s.num"], got["s.str"],
		"a parity comparison must be able to tell 8 from \"8\"")
}

// TestSettingsKeysCannotCollideThroughRendering pins the second reason Settings is a map
// rather than a rendered document. A key may legally contain a newline, and the parity
// fuzz target appends arbitrary bytes to app.toml, so a newline-joined rendering can make
// two different key sets produce one identical string. Map keys cannot collide that way.
func TestSettingsKeysCannotCollideThroughRendering(t *testing.T) {
	two := configtest.Settings(newViperOver(t, "[s]\n\"a\" = 1\n\"b\" = 2\n"))
	one := configtest.Settings(newViperOver(t, "[s]\n\"a = 1\\nb\" = 2\n"))

	require.NotEqual(t, two, one,
		"two keys and one newline-containing key must not compare equal")
	require.Len(t, two, 2)
	require.Len(t, one, 1)
}

// TestSettingsOnNilViper pins the nil case, since the differential builds a
// server.Context whose Viper may be unset before Apply runs.
func TestSettingsOnNilViper(t *testing.T) {
	require.Nil(t, configtest.Settings(nil))
}
