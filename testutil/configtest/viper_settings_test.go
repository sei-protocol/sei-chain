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

// newViperOver returns a viper whose only layer is the given app.toml body: no prefix,
// no AutomaticEnv, no replacer. Settings's interaction with the environment is pinned
// against the real server viper in cmd/seid/cmd, so nothing here needs an env layer.
func newViperOver(t *testing.T, body string) *viper.Viper {
	t.Helper()
	return newViperOverFile(t, writeAppTOML(t, body))
}

// writeAppTOML writes body to an app.toml in a fresh temp directory and returns its path.
func writeAppTOML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write app.toml: %v", err)
	}
	return path
}

// newViperOverFile returns a viper that has read the app.toml at path, the same single
// file layer newViperOver builds. It is separate so a test can read one file many times:
// the nondeterminism such a test is after lives in viper's map iteration, not in the
// parse, so re-reading one path varies only the thing under study.
func newViperOverFile(t *testing.T, path string) *viper.Viper {
	t.Helper()
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
// Both halves are assertions, and the AllSettings half holds on every single read rather
// than over the sample. Whichever order the re-nesting visits the two keys in, one
// value is destroyed: reaching "giga.x" through a scalar "giga" replaces that scalar with
// a fresh map, and writing "giga" over an existing sub-tree discards the sub-tree. So the
// re-nested tree always holds one leaf fewer than AllKeys has keys. Only *which* value is
// lost varies with the ordering, which is the instability the shape count reports.
//
// Asserting the loss rather than the variation is what makes this a regression guard: it
// cannot depend on hitting both orderings within the sample, and a viper that fixed the
// collision fails here instead of quietly making the test meaningless.
func TestSettingsIsStableWhereAllSettingsIsNot(t *testing.T) {
	const body = `
[section]
"giga" = 1
"giga.x" = 2
`
	// Small on purpose. Both assertions below are deterministic per read — Settings cannot
	// vary, and the leaf loss holds whichever way deepSearch re-nested — so the sample size
	// feeds only the informational shape count. At the measured ~4:1 split, 20 reads show
	// both orderings better than 99% of the time, and 180 fewer parses keeps this out of
	// the package's runtime.
	const runs = 20

	// One file, read many times. Writing it once keeps the parse out of the experiment,
	// leaving map iteration order as the only thing that differs between reads.
	path := writeAppTOML(t, body)

	// The shape fingerprint is fmt.Sprint of a map, which is stable only because fmt
	// sorts map keys before printing (Go 1.12+). Worth stating in a test whose whole
	// premise is that map ordering is untrustworthy: the ordering fmt hides is the one
	// this test relies on, and the ordering AllSettings exposes is the one it measures.
	// Without the sort, every read would look like a distinct shape and the counts below
	// would be meaningless rather than wrong.

	settingsShapes := map[string]int{}
	allSettingsShapes := map[string]int{}
	for i := 0; i < runs; i++ {
		v := newViperOverFile(t, path)
		settingsShapes[fmt.Sprint(configtest.Settings(v))]++

		all := v.AllSettings()
		allSettingsShapes[fmt.Sprint(all)]++

		keys := v.AllKeys()
		require.Len(t, keys, 2,
			"the fixture must present the prefix collision this test is about; got keys %v", keys)
		require.Equal(t, len(keys)-1, countLeaves(all),
			"AllSettings must lose exactly one of the two colliding values on every read, "+
				"whichever way it re-nested; got %v for keys %v. This characterizes viper, "+
				"not this repo: if it fails after a viper upgrade, AllSettings may have "+
				"fixed the prefix collision, in which case delete this assertion rather "+
				"than working around it — Settings stays correct either way", all, keys)
	}

	require.Len(t, settingsShapes, 1,
		"Settings must return one shape across %d reads of one file; got %d: %v",
		runs, len(settingsShapes), settingsShapes)

	t.Logf("AllSettings returned %d distinct shapes across %d reads, each of them lossy: %v",
		len(allSettingsShapes), runs, allSettingsShapes)
}

// countLeaves counts the non-map values in a nested settings tree, which is how many of
// AllKeys's keys survived being re-nested into it.
func countLeaves(m map[string]any) int {
	n := 0
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			n += countLeaves(sub)
			continue
		}
		n++
	}
	return n
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
// rather than a rendered document: a rendering can collide where the underlying key sets
// differ, and a map cannot.
//
// The fixtures are built to reach that collision rather than merely to differ, which is
// the whole difficulty. DumpViper renders each key as `key = type(value)` and joins with
// newlines, and a TOML key may legally contain a newline — so a single key whose text
// reproduces one map's entire rendering makes the two documents byte-identical. The
// second fixture is exactly that: one root key spelled `s.a = int64(1)\ns.b` holding
// int64(2), which renders as `s.a = int64(1)\ns.b = int64(2)` — the same two lines the
// first fixture's two keys produce.
//
// An earlier version of this test used `"a = 1\nb" = 2` and asserted only that the two
// maps differ. That passed for a reason unrelated to the property: the renderings were
// never equal, because DumpViper wraps the value as int64(1) while the key text carried a
// bare 1. Two maps of different sizes are unequal, so the assertion held while the
// collision it exists to demonstrate was never constructed. That is worth recording here,
// because it is the failure this file is otherwise about: an assertion that passes for a
// reason its own comment does not claim.
func TestSettingsKeysCannotCollideThroughRendering(t *testing.T) {
	twoKeys := newViperOver(t, "[s]\n\"a\" = 1\n\"b\" = 2\n")
	oneKey := newViperOver(t, "\"s.a = int64(1)\\ns.b\" = 2\n")

	// The premise: the two renderings really are identical. Without this the assertion
	// below is the vacuous one described above.
	require.Equal(t, configtest.DumpViper(twoKeys), configtest.DumpViper(oneKey),
		"the fixtures must reach the rendering collision this test is about, or the "+
			"comparison below proves nothing about it")

	// The property: Settings keeps them apart anyway, because it compares map keys rather
	// than a joined document.
	two, one := configtest.Settings(twoKeys), configtest.Settings(oneKey)
	require.NotEqual(t, two, one,
		"Settings must distinguish key sets whose rendering collides")
	require.Len(t, two, 2)
	require.Len(t, one, 1, "a newline-containing key must stay a single entry")
}

// TestSettingsOnNilViperPanics pins the nil case as a failure rather than a tolerance,
// since the differential builds a server.Context whose Viper stays nil until Apply
// populates it. An empty map would let two such contexts compare equal, so the
// differential's strongest premise would report success on a boot that never ran. The
// panic is what makes the guard at those call sites redundant rather than load-bearing.
func TestSettingsOnNilViperPanics(t *testing.T) {
	require.Panics(t, func() { configtest.Settings(nil) })
}
