package cmd

import (
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
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
// against these rows rather than discovered against a production node. That correction is
// tracked on PLT-775, so the fix has an anchor that outlives this comment.
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
//
// Two of the three links in that chain are asserted, and the third is described. This
// file asserts the shadow itself and, through gigaconfig.ReadConfig on the resolved
// viper, that the nil read leaves enabled at true. app's
// TestGigaExecutorEnabledDrivesLastResultsHashValidation asserts that enabled reaches the
// atomic, through a real app construction. Only the join between them is prose here,
// because observing it needs a running node, which these tests deliberately do not do.

// sectionEnvVar returns the environment variable that shadows the given dotted path,
// derived the way the server viper derives it rather than built by hand.
//
// Built by hand it is easy to get wrong in a way that reads as "the defect does not
// exist". The replacer runs over the whole prefixed name, prefix included, and the prefix
// is the running binary's basename — "cmd.test" under go test. Folding only the key and
// joining it to the prefix therefore leaves that dot in place, yielding
// CMD.TEST_GIGA_EXECUTOR, which no environment lookup matches and which shadows nothing.
func sectionEnvVar(t *testing.T, path string) string {
	t.Helper()
	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("resolve server env prefix: %v", err)
	}
	return configtest.ServerEnvKey(prefix, path)
}

// homeWithAppTOML returns a fresh fixture home whose app.toml holds the given body.
// Booting it is applyLegacy's job.
func homeWithAppTOML(t *testing.T, body string) *configtest.Home {
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
// under test.
func TestSectionEnvVarShadowsItsWholeSection(t *testing.T) {
	configtest.Isolate(t)

	const body = `[giga_executor]
enabled = false
occ_enabled = false

[evm]
max_log_bytes = 100
`
	shadowVar := sectionEnvVar(t, "giga_executor")

	// Baseline: no shadowing variable, so the operator's written values win.
	base := applyLegacy(t, homeWithAppTOML(t, body), nil)
	require.NoError(t, base.err)
	require.Equal(t, false, base.ctx.Viper.Get("giga_executor.enabled"),
		"without a shadowing variable the operator's written value must win")

	for _, value := range []string{"true", "false", "1", "0", "anything-at-all"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv(shadowVar, value)
			got := applyLegacy(t, homeWithAppTOML(t, body), nil)
			require.NoError(t, got.err, "shadowing must not refuse the boot")
			v := got.ctx.Viper

			// Every key under the shadowed section resolves to nothing, whatever the
			// variable's value. That is what distinguishes this from an override.
			require.Nil(t, v.Get("giga_executor.enabled"),
				"%s=%q must make giga_executor.enabled resolve to nothing", shadowVar, value)
			require.Nil(t, v.Get("giga_executor.occ_enabled"))
			require.False(t, v.IsSet("giga_executor.enabled"))

			// What the nil read costs, asserted through the section's real reader
			// rather than inferred from it. ReadConfig is presence-guarded, so a key
			// that resolves to nothing leaves the in-code default standing, and
			// enabled comes back true from a file that says false. This is the step
			// the consensus consequence in the header rests on.
			cfg, err := gigaconfig.ReadConfig(v)
			require.NoError(t, err, "a shadowed section must not make the reader fail")
			require.True(t, cfg.Enabled,
				"%s=%q must leave giga_executor.enabled at its in-code default of true, "+
					"against an app.toml that sets it to false", shadowVar, value)
			require.Equal(t, gigaconfig.DefaultConfig, cfg,
				"a shadowed section must resolve to exactly the in-code defaults, so no "+
					"value the operator wrote survives anywhere in it")

			// A sibling section is untouched: the shadow is scoped to the prefix the
			// variable names, not to the whole file.
			require.Equal(t, int64(100), v.Get("evm.max_log_bytes"),
				"the shadow must not reach a section the variable does not name")

			// Enumeration still reports the shadowed keys as present, which is why a
			// reader cannot infer "in effect" from "enumerated".
			require.Contains(t, v.AllKeys(), "giga_executor.enabled",
				"AllKeys must still list a shadowed key")

			// The same pair, through configtest.Settings, because this is the only place
			// it can be asserted. Settings' coverage argument is that AllSettings omits a
			// key whose Get returns nil while Settings records it as an explicit nil
			// entry — and a key that enumerates but resolves to nothing is what the
			// environment shadow produces and nothing else does, so the vipers in
			// configtest's own tests cannot construct one. Contains before the nil check:
			// indexing an absent key also yields nil, so the two together are what
			// distinguish "recorded as nil" from "not recorded".
			settings := configtest.Settings(v)
			require.Contains(t, settings, "giga_executor.enabled",
				"Settings must record a shadowed key, which is the entry AllSettings drops")
			require.Nil(t, settings["giga_executor.enabled"],
				"Settings must record a shadowed key as an explicit nil entry")
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

	home := homeWithAppTOML(t, "[giga_executor]\nenabled = false\n")
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

	home := homeWithAppTOML(t, "[giga_executor]\nocc_enabled = false\n")

	t.Setenv(sectionEnvVar(t, "giga_executor.occ_enabled"), "true")
	delivered := applyLegacy(t, home, nil)
	require.NoError(t, delivered.err)
	require.Equal(t, "true", delivered.ctx.Viper.Get("giga_executor.occ_enabled"),
		"a variable naming the full key path must deliver its value (env over file), and it "+
			"arrives as the untyped string it was written as — the same key read from the file "+
			"layer resolves to a bool, so which layer a value came from changes its Go type")
}

// TestShadowFoldsSectionPunctuation records that the shadow does not care how the
// section is punctuated, because the replacer folds "." and "-" to "_" before the
// lookup. A dashed section is shadowed by the same variable-name shape as an
// underscored one — which is also why two differently-punctuated names cannot be
// distinguished by environment delivery.
//
// It also records the sharper half of the consequence, because this key's reader is
// shaped differently from giga_executor's. GetConfig reads it unguarded —
// v.GetBool("state-commit.sc-enable") at sei-cosmos/server/config/config.go:621, with no
// presence check — so a nil read is not a fallback to an in-code default, it is
// GetBool(nil), which is false. The two sections therefore fail in opposite directions
// from one mechanism: a shadowed giga_executor.enabled keeps its true default and
// silently enables what the operator disabled, while a shadowed state-commit.sc-enable
// resolves false and silently disables what the operator enabled. Which direction a
// section takes depends only on whether its reader guards the read.
//
// The baseline is what makes the nil assertion mean anything. A nil is evidence of
// shadowing only if the key is known to resolve without the variable set — otherwise a
// fixture typo or a section rename would satisfy the assertion while the dash-folding
// property it exists to pin went untested.
func TestShadowFoldsSectionPunctuation(t *testing.T) {
	configtest.Isolate(t)

	const body = "[state-commit]\nsc-enable = true\n"
	shadowVar := sectionEnvVar(t, "state-commit")
	require.Contains(t, shadowVar, "STATE_COMMIT",
		"the replacer must fold the dash to an underscore")

	// Baseline, on its own fixture home: the dashed key resolves, and the reader agrees.
	base := applyLegacy(t, homeWithAppTOML(t, body), nil)
	require.NoError(t, base.err)
	require.Equal(t, true, base.ctx.Viper.Get("state-commit.sc-enable"),
		"without a shadowing variable the dashed key must resolve to the written value")
	require.True(t, base.ctx.Viper.GetBool("state-commit.sc-enable"),
		"the reader must see the operator's value when nothing shadows it")

	t.Setenv(shadowVar, "x")
	got := applyLegacy(t, homeWithAppTOML(t, body), nil)
	require.NoError(t, got.err)
	require.Nil(t, got.ctx.Viper.Get("state-commit.sc-enable"),
		"%s must shadow a dashed section the same way it shadows an underscored one", shadowVar)
	require.False(t, got.ctx.Viper.GetBool("state-commit.sc-enable"),
		"%s must make the unguarded reader resolve false, against an app.toml that sets it "+
			"true — the opposite direction from giga_executor, whose guarded reader keeps its "+
			"true default instead", shadowVar)
}
