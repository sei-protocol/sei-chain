package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
)

// Environment-variable shadowing of a whole config section.
//
// A non-empty SEID_<SECTION> variable makes every key under that section resolve to
// nothing, so the operator's written value in app.toml is discarded and the reader
// falls back to its in-code default. The variable's own value is never used: setting
// the variable to "false" produces the same result as setting it to "true" or to any
// other string. So this is not the environment overriding the file, which is the
// documented and intended precedence — it is the file's value being dropped and a
// third value, belonging to no layer, taking effect.
//
// The mechanism is viper's isPathShadowedInAutoEnv, which walks every proper prefix of
// a dotted key and, if any prefixed variable is set, returns before the config file is
// consulted. It cannot distinguish "SEID_GIGA_EXECUTOR names a scalar, so
// giga_executor.enabled cannot exist" from "SEID_GIGA_EXECUTOR is unrelated to the key
// I was asked for".
//
// These rows pin the behavior; they do not endorse it. The legacy path has shipped, and
// changing how configuration resolves could silently break an operator who has come to
// depend on the current answer, so the behavior is recorded here and left alone.
// SeiConfigManager corrects it when it owns resolution, and the divergence is ratified
// against these rows rather than discovered against a production node.
//
// The consequence is worth stating where the pin lives, because it is what makes the
// pin worth having rather than trivia. giga_executor.enabled defaults to true
// (giga/executor/config/config.go), its reader is presence-guarded so a nil read keeps
// that default, and app.New feeds it to tmtypes.SkipLastResultsHashValidation.Store —
// which gates whether the node compares block.LastResultsHash against
// state.LastResultsHash (sei-tendermint/internal/state/validation.go). An operator who
// writes `enabled = false` and has any non-empty SEID_GIGA_EXECUTOR in the process
// environment therefore gets giga enabled and that consensus comparison relaxed, with
// nothing in any log saying so.

// sectionEnvVar returns the environment variable that shadows the given dotted path,
// derived the way the server viper derives it rather than built by hand.
//
// Built by hand it is easy to get wrong in a way that reads as "the defect does not
// exist": the replacer runs over the whole prefixed name, so folding only the key and
// then joining with an underscore yields SEID_GIGA.EXECUTOR, which matches nothing and
// shadows nothing.
func sectionEnvVar(t *testing.T, path string) string {
	t.Helper()
	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("resolve server env prefix: %v", err)
	}
	return configtest.ServerEnvKey(prefix, path)
}

// bootWithAppTOML boots one fixture home carrying the given app.toml body through the
// legacy manager and returns the resolved viper every appOpts.Get() call site reads.
func bootWithAppTOML(t *testing.T, body string) *configtest.Home {
	t.Helper()
	home := configtest.NewHome(t)
	home.WriteAppTOML(t, []byte(body))
	return home
}

// TestSectionEnvVarShadowsItsWholeSection records the shadow itself: the section's keys
// stop resolving, a sibling section is untouched, and enumeration keeps listing the
// shadowed keys.
//
// Every row boots its own fixture home, the baseline included: the legacy path treats a
// node directory as read-write while it reads it and creates config.toml on first boot,
// so one home shared across the rows would vary a second input alongside the variable
// under test (testutil/configtest/AGENTS.md).
func TestSectionEnvVarShadowsItsWholeSection(t *testing.T) {
	configtest.Isolate(t)

	const body = `minimum-gas-prices = "0.1usei"

[giga_executor]
enabled = false
occ_enabled = false

[evm]
max_log_bytes = 100
`
	shadowVar := sectionEnvVar(t, "giga_executor")

	// Baseline: no shadowing variable, so the operator's written values win.
	base := applyLegacy(t, bootWithAppTOML(t, body), nil)
	require.NoError(t, base.err)
	require.Equal(t, false, base.ctx.Viper.Get("giga_executor.enabled"),
		"without a shadowing variable the operator's written value must win")

	for _, value := range []string{"true", "false", "1", "0", "anything-at-all"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv(shadowVar, value)
			got := applyLegacy(t, bootWithAppTOML(t, body), nil)
			require.NoError(t, got.err, "shadowing must not refuse the boot")
			v := got.ctx.Viper

			// Every key under the shadowed section resolves to nothing, whatever the
			// variable's value. That is what distinguishes this from an override.
			require.Nil(t, v.Get("giga_executor.enabled"),
				"%s=%q must make giga_executor.enabled resolve to nothing", shadowVar, value)
			require.Nil(t, v.Get("giga_executor.occ_enabled"))
			require.False(t, v.IsSet("giga_executor.enabled"))

			// A sibling section is untouched: the shadow is scoped to the prefix the
			// variable names, not to the whole file.
			require.Equal(t, int64(100), v.Get("evm.max_log_bytes"),
				"the shadow must not reach a section the variable does not name")

			// Enumeration still reports the shadowed keys as present, which is why a
			// reader cannot infer "in effect" from "enumerated".
			require.True(t, slices.Contains(v.AllKeys(), "giga_executor.enabled"),
				"AllKeys must still list a shadowed key")
		})
	}
}

// TestEmptySectionEnvVarDoesNotShadow pins the boundary: an empty value is not a
// shadow. viper's env lookup treats an empty value as absent unless AllowEmptyEnv is
// set, and seid does not set it. Recorded because the empty and non-empty cases behave
// differently, and only the pair distinguishes the real rule from "the variable
// exists".
func TestEmptySectionEnvVarDoesNotShadow(t *testing.T) {
	configtest.Isolate(t)

	home := bootWithAppTOML(t, "[giga_executor]\nenabled = false\n")
	t.Setenv(sectionEnvVar(t, "giga_executor"), "")

	got := applyLegacy(t, home, nil)
	require.NoError(t, got.err)
	require.Equal(t, false, got.ctx.Viper.Get("giga_executor.enabled"),
		"an empty shadowing variable must not shadow; the operator's value must still win")
	require.True(t, got.ctx.Viper.IsSet("giga_executor.enabled"))
}

// TestFullKeyEnvVarDeliversRatherThanShadows pins the other boundary, and it is the row
// that shows the shadow is not precedence. A variable naming the key's FULL path
// delivers its value, which is the intended env-over-file precedence. A variable naming
// a proper prefix of that path delivers nothing and discards the file's value. Same
// key, same environment mechanism, opposite outcomes.
func TestFullKeyEnvVarDeliversRatherThanShadows(t *testing.T) {
	configtest.Isolate(t)

	home := bootWithAppTOML(t, "[giga_executor]\nocc_enabled = false\n")

	t.Setenv(sectionEnvVar(t, "giga_executor.occ_enabled"), "true")
	delivered := applyLegacy(t, home, nil)
	require.NoError(t, delivered.err)
	require.Equal(t, "true", delivered.ctx.Viper.Get("giga_executor.occ_enabled"),
		"a variable naming the full key path must deliver its value (env over file), and it "+
			"arrives as the untyped string it was written as — the same key read from the file "+
			"layer resolves to a bool, so which layer a value came from changes its Go type")
}

// TestShadowFoldsSectionPunctuation records that the shadow does not care how the
// section is punctuated, because the replacer folds ".", "-" and "_" to "_" before the
// lookup. A dashed section is shadowed by the same variable-name shape as an
// underscored one — which is also why two differently-punctuated names cannot be
// distinguished by environment delivery.
func TestShadowFoldsSectionPunctuation(t *testing.T) {
	configtest.Isolate(t)

	home := bootWithAppTOML(t, "[state-commit]\nsc-enable = true\n")
	shadowVar := sectionEnvVar(t, "state-commit")
	require.True(t, strings.Contains(shadowVar, "STATE_COMMIT"),
		"the replacer must fold the dash to an underscore: got %q", shadowVar)

	t.Setenv(shadowVar, "x")
	got := applyLegacy(t, home, nil)
	require.NoError(t, got.err)
	require.Nil(t, got.ctx.Viper.Get("state-commit.sc-enable"),
		"%s must shadow a dashed section the same way it shadows an underscored one", shadowVar)
}
