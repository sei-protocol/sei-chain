package cmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// This file is the equality foundation for the two managers: everything here
// compares what legacy resolves against what v2 resolves, on one home, through the
// two channels the rest of the boot reads.
//
// It runs on the same harness as the characterization suite (testutil/configtest),
// which is what makes the comparison trustworthy. Isolate pins the process
// environment, since the legacy reader answers to environment variables whose prefix
// follows the test binary's own name, and NewHome gives a node directory the test
// controls byte for byte, since the legacy reader writes into a home while reading it.
// See testutil/configtest/AGENTS.md.
//
// The verdicts stay require.Equal rather than the harness dump helpers. For the
// *tmcfg.Config struct that is a stronger comparison, because reflect.DeepEqual
// reaches unexported fields a rendered dump does not, and a dump is the right
// instrument for a recorded golden rather than for a relative legacy-versus-v2
// equality.

// errStopPreRun aborts the command after the config-resolution PreRunE, before
// StartCmd's RunE tries to boot a node.
var errStopPreRun = errors.New("stop after prerun")

// runConfigManager runs mgr.Apply inside a StartCmd's PreRunE against home supplied
// via the --home flag, using seid's real app-config template, and returns the
// populated server context (the two channels start.go/app.New consume).
func runConfigManager(t *testing.T, mgr configmanager.ConfigManager, home *configtest.Home) *server.Context {
	t.Helper()
	return execConfigManager(t, mgr, startCmdForHome(t, home))
}

// startCmdForHome builds a StartCmd with --home set to the fixture home.
func startCmdForHome(t *testing.T, home *configtest.Home) *cobra.Command {
	t.Helper()
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, home.Root))
	return cmd
}

// runConfigManagerEnvHome is runConfigManager's twin that supplies the home through
// the environment instead of --home, exercising the SetEnvPrefix/AutomaticEnv
// machinery that v2's resolveHomeDir mirrors from the legacy handler. The flag-driven
// path never touches it.
//
// The key comes from the harness rather than being spelled here, because the prefix
// is the running binary's basename and both the legacy handler and resolveHomeDir
// derive it that way. Hardcoding "seid" would pass under a binary named seid and say
// nothing about the resolution actually under test.
func runConfigManagerEnvHome(t *testing.T, mgr configmanager.ConfigManager, home *configtest.Home) *server.Context {
	t.Helper()
	prefix, err := configtest.ServerEnvPrefix()
	require.NoError(t, err)
	t.Setenv(configtest.ServerEnvKey(prefix, flags.FlagHome), home.Root)

	// Leave --home unset: an unchanged flag default ranks below AutomaticEnv in
	// viper's precedence, so the env value is what resolves.
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	return execConfigManager(t, mgr, cmd)
}

// execConfigManager runs mgr.Apply on the happy path (Apply succeeds; boot is
// aborted with errStopPreRun) and returns the populated server context. The caller
// configures how home is supplied (flag vs env) on cmd beforehand.
func execConfigManager(t *testing.T, mgr configmanager.ConfigManager, cmd *cobra.Command) *server.Context {
	t.Helper()
	ctx, err := runManager(t, mgr, cmd)
	require.NoError(t, err)
	return ctx
}

// runManager runs mgr.Apply inside cmd's PreRunE and returns the populated server
// context and the error Apply returned. Apply is the only boot-refusing call, so on
// the happy path it returns nil and boot is aborted with errStopPreRun; on a real
// config error it returns that error and runManager surfaces it unchanged. Advisory
// diagnostics go to seilog (not cmd's stderr), so they are not captured here — the
// invariants under test are the returned context and error, not the log text.
func runManager(t *testing.T, mgr configmanager.ConfigManager, cmd *cobra.Command) (*server.Context, error) {
	t.Helper()
	template, appCfg := initAppConfig()
	cmd.SetErr(io.Discard) // swallow cobra's own error echo; advisory logs go to seilog

	var applyErr error
	cmd.PreRunE = func(c *cobra.Command, _ []string) error {
		if applyErr = mgr.Apply(c, template, appCfg); applyErr != nil {
			return applyErr
		}
		return errStopPreRun
	}

	serverCtx := &server.Context{}
	ctx := context.WithValue(context.Background(), server.ServerContextKey, serverCtx)
	execErr := cmd.ExecuteContext(ctx)
	if applyErr == nil {
		require.ErrorIs(t, execErr, errStopPreRun)
	}
	return serverCtx, applyErr
}

// seedDefaultConfig returns a home carrying a complete, realistic config (all Sei
// sections), generated by letting the legacy creator write into a fresh home.
func seedDefaultConfig(t *testing.T) *configtest.Home {
	t.Helper()
	home := configtest.NewHome(t)
	_ = runConfigManager(t, configmanager.LegacyConfigManager{}, home)
	return home
}

// appendToAppTOML appends s to the home's app.toml. Every corpus mutation targets
// app.toml, so these three helpers name that rather than taking a path.
func appendToAppTOML(t *testing.T, home *configtest.Home, s string) {
	t.Helper()
	b, ok := home.Read(t, "app.toml")
	require.True(t, ok, "app.toml must exist before it is appended to")
	home.WriteAppTOML(t, append(b, []byte(s)...))
}

// prependToAppTOML prepends s to the home's app.toml.
func prependToAppTOML(t *testing.T, home *configtest.Home, s string) {
	t.Helper()
	b, ok := home.Read(t, "app.toml")
	require.True(t, ok, "app.toml must exist before it is prepended to")
	home.WriteAppTOML(t, append([]byte(s), b...))
}

// replaceInAppTOML replaces oldStr with newStr in the home's app.toml, asserting
// oldStr was present so a corpus mutation can never silently become a no-op.
func replaceInAppTOML(t *testing.T, home *configtest.Home, oldStr, newStr string) {
	t.Helper()
	b, ok := home.Read(t, "app.toml")
	require.True(t, ok, "app.toml must exist before it is edited")
	require.Contains(t, string(b), oldStr, "replace target %q not found — fixture would be vacuous", oldStr)
	home.WriteAppTOML(t, []byte(strings.ReplaceAll(string(b), oldStr, newStr)))
}

// corpusCase is one realistic on-disk config shape, applied to a freshly-seeded
// default home. It is the shared unit both the table-driven differential and the
// fuzz target consume, so the set of "interesting shapes" lives in one place.
type corpusCase struct {
	name   string
	mutate func(t *testing.T, home *configtest.Home)
}

// configCorpus is the single source of the config shapes the parity proof runs over.
// Each case mutates a default home in place; parity must hold for all of them because
// v2 re-enters the legacy reader regardless of what it read.
func configCorpus() []corpusCase {
	return []corpusCase{
		{"default", func(t *testing.T, home *configtest.Home) {}},
		{"leading-comments-and-blanks", func(t *testing.T, home *configtest.Home) {
			prependToAppTOML(t, home, "# corpus: a leading comment\n\n")
		}},
		{"unknown-section", func(t *testing.T, home *configtest.Home) {
			appendToAppTOML(t, home, "\n[sei-corpus-unknown]\nkey = \"value\"\n")
		}},
		{"quoted-scalar", func(t *testing.T, home *configtest.Home) {
			// A real numeric field written as a quoted string (sei-config #36's
			// lenient-decode shape). Both paths re-enter the same legacy reader, so
			// the channels match regardless — parity holds because v2's read is
			// advisory. Coercion of quoted primitives is verified in sei-config's
			// own tests; the differential only observes channel parity, not the
			// advisory read's outcome.
			replaceInAppTOML(t, home, "ss-keep-recent = 100000", `ss-keep-recent = "100000"`)
		}},
		{"cosmos-only-write-mode", func(t *testing.T, home *configtest.Home) {
			// The version-skew class: a config carrying the deprecated
			// state-commit.sc-write-mode "cosmos_only". sei-config still accepts it
			// as valid, so v2 raises no diagnostic today; the point here is that
			// both managers read it identically (parity). It becomes a *caught*
			// case only once fatal validation + a sei-config deprecation rule land.
			replaceInAppTOML(t, home, `sc-write-mode = "memiavl_only"`, `sc-write-mode = "cosmos_only"`)
		}},
	}
}

// TestConfigManagerLegacyVsV2Differential is the core safety property: the v2
// manager must produce the SAME consumed config as the legacy path. v2 reads the
// config (to validate it) and then re-enters the legacy reader on the operator's
// ORIGINAL files — it does not rewrite — so the two paths read the SAME home and any
// difference is a real divergence, not a path artifact.
//
// It compares parsed semantics:
//   - serverCtx.Config (the *tmcfg.Config the node runs on), and
//   - serverCtx.Viper.AllSettings() (the AppOptions every Sei section reads via
//     appOpts.Get), both at end-of-PersistentPreRunE and after the start.go
//     chain-id mutation.
func TestConfigManagerLegacyVsV2Differential(t *testing.T) {
	configtest.Isolate(t)

	// Both managers read the same home, carrying a complete realistic config.
	home := seedDefaultConfig(t)
	legacyCtx := runConfigManager(t, configmanager.LegacyConfigManager{}, home)
	v2Ctx := runConfigManager(t, configmanager.SeiConfigManager{}, home)

	require.Equal(t, legacyCtx.Config, v2Ctx.Config,
		"serverCtx.Config differs between legacy and v2")
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(),
		"serverCtx.Viper settings differ between legacy and v2")

	// The start.go chain-id mutation is identical on both vipers; assert parity
	// holds after it too (covers the post-mutation snapshot).
	const chainID = "differential-test-1"
	legacyCtx.Viper.Set(flags.FlagChainID, chainID)
	v2Ctx.Viper.Set(flags.FlagChainID, chainID)
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(),
		"settings diverge after the start.go chain-id mutation")
}

// TestConfigManagerLegacyVsV2Differential_EnvHome exercises the env-precedence half
// of resolveHomeDir's mirror of the legacy handler — the flag-driven differential
// above never touches SetEnvPrefix/AutomaticEnv. When home is supplied via the
// environment (not --home), v2 must resolve the SAME home the legacy handler does;
// otherwise v2 would advisorily validate one dir while the re-entered legacy reader
// boots on another — a silent drift the advisory design cannot surface (no error, no
// diagnostic). This pins the seam so a future change to the legacy env-resolution
// can't diverge undetected.
func TestConfigManagerLegacyVsV2Differential_EnvHome(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	// Populate a complete realistic config in home via the fresh-home legacy
	// creator, driven entirely through the env var (no --home).
	_ = runConfigManagerEnvHome(t, configmanager.LegacyConfigManager{}, home)

	legacyCtx := runConfigManagerEnvHome(t, configmanager.LegacyConfigManager{}, home)
	v2Ctx := runConfigManagerEnvHome(t, configmanager.SeiConfigManager{}, home)

	// Non-vacuous guard: the env var actually drove resolution. If the key were
	// wrong, both would fall back to StartCmd's "/foobar" default (and the legacy
	// creator would fail writing under it) — this asserts the env path resolved to
	// the fixture home, for both managers.
	require.Equal(t, home.Root, v2Ctx.Viper.GetString(flags.FlagHome),
		"env-provided home did not drive v2 resolution")
	require.Equal(t, home.Root, legacyCtx.Viper.GetString(flags.FlagHome),
		"env-provided home did not drive legacy resolution")

	require.Equal(t, legacyCtx.Config, v2Ctx.Config,
		"serverCtx.Config differs between legacy and v2 on the env-home path")
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(),
		"serverCtx.Viper settings differ between legacy and v2 on the env-home path")
}

// TestConfigManagerLegacyVsV2Differential_Corpus widens the parity proof from the
// single default fixture to a corpus of realistic on-disk shapes. Parity is by
// construction (v2 re-enters the legacy reader), so any shape an operator could
// present must produce identical channels — including shapes that exercise
// sei-config's own reader (quoted scalars, unknown keys), whose advisory read still
// must not perturb what the node boots on.
func TestConfigManagerLegacyVsV2Differential_Corpus(t *testing.T) {
	for _, tc := range configCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			configtest.Isolate(t)
			home := seedDefaultConfig(t)
			tc.mutate(t, home)

			legacyCtx := runConfigManager(t, configmanager.LegacyConfigManager{}, home)
			v2Ctx := runConfigManager(t, configmanager.SeiConfigManager{}, home)

			require.Equal(t, legacyCtx.Config, v2Ctx.Config,
				"serverCtx.Config differs between legacy and v2 (%s)", tc.name)
			require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(),
				"serverCtx.Viper settings differ between legacy and v2 (%s)", tc.name)
		})
	}
}

// TestConfigManagerV2AdvisoryNeverRefusesBoot pins the advisory invariant: on a valid
// config, v2 boots exactly as legacy does (Apply returns nil, both channels match),
// regardless of any diagnostics it prints. v2 adds observability, never a new boot
// outcome.
func TestConfigManagerV2AdvisoryNeverRefusesBoot(t *testing.T) {
	configtest.Isolate(t)
	home := seedDefaultConfig(t)

	v2Ctx, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))
	require.NoError(t, v2Err, "advisory validation must never refuse boot on a valid config")

	legacyCtx := runConfigManager(t, configmanager.LegacyConfigManager{}, home)
	require.Equal(t, legacyCtx.Config, v2Ctx.Config)
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings())
}

// TestConfigManagerV2FreshHomeBoots exercises the fresh-home first-boot path: v2's
// advisory read hits os.ErrNotExist (no config yet), silently skips, then re-enters
// the legacy handler, which creates the files. It must not refuse boot. Every other
// test pre-seeds the config, so this is the only cover of the ErrNotExist branch —
// the common case for a brand-new node, and the home here stays deliberately unseeded.
func TestConfigManagerV2FreshHomeBoots(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	v2Ctx, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))
	require.NoError(t, v2Err, "v2 on a fresh home must not refuse boot on the missing-config read")
	require.NotNil(t, v2Ctx.Config)
}

// TestConfigManagerV2AdvisoryReadErrorMatchesLegacy pins the other half of the
// invariant: when the config is unreadable, v2 must not mask the failure or invent a
// new one. It logs an advisory read error (via seilog), then re-enters the legacy
// handler and returns exactly the error legacy returns.
func TestConfigManagerV2AdvisoryReadErrorMatchesLegacy(t *testing.T) {
	configtest.Isolate(t)
	home := seedDefaultConfig(t)
	home.WriteConfigTOML(t, []byte("this is ] not [ valid toml"))

	_, legacyErr := runManager(t, configmanager.LegacyConfigManager{}, startCmdForHome(t, home))
	_, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))

	require.Error(t, legacyErr, "corrupt config.toml should fail the legacy reader")
	require.Equal(t, legacyErr.Error(), v2Err.Error(),
		"v2 must return the same boot error as legacy, not mask or add one")
}

// FuzzConfigManagerLegacyVsV2Parity is the exhaustive form of the corpus: it crosses
// every corpus shape with an arbitrary appended app.toml suffix, and asserts legacy
// and v2 reach the same outcome — identical channels when both succeed, the identical
// error when both fail. Parity is by construction, so the fuzzer should never find a
// divergence. Under `go test` (no -fuzz) it runs the seed corpus (each shape × a few
// suffixes), a deterministic differential in CI; under -fuzz it explores suffixes
// against every shape.
//
// The suffix is deliberately ungated: feeding bytes that do not form a valid TOML
// document is the point, since the property is that both managers fail the same way.
// The harness's writability guards belong to targets that build a document from a
// fuzzed value, not to one appending arbitrary bytes on purpose.
func FuzzConfigManagerLegacyVsV2Parity(f *testing.F) {
	corpus := configCorpus()
	for i := range corpus {
		f.Add(uint(i), "")
		f.Add(uint(i), "\n# a trailing comment\n")
		f.Add(uint(i), "\nnot valid toml ][")
	}

	// corpusIdx is unsigned so a fuzzed index maps to a case with a plain modulo
	// — no sign guard, no math.MinInt negation edge.
	f.Fuzz(func(t *testing.T, corpusIdx uint, appTOMLSuffix string) {
		configtest.Isolate(t)
		tc := corpus[corpusIdx%uint(len(corpus))]

		home := seedDefaultConfig(t)
		tc.mutate(t, home)
		appendToAppTOML(t, home, appTOMLSuffix)

		legacyCtx, legacyErr := runManager(t, configmanager.LegacyConfigManager{}, startCmdForHome(t, home))
		v2Ctx, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))

		if (legacyErr == nil) != (v2Err == nil) {
			t.Fatalf("divergent outcome (case %q, suffix %q): legacyErr=%v v2Err=%v", tc.name, appTOMLSuffix, legacyErr, v2Err)
		}
		if legacyErr != nil {
			require.Equal(t, legacyErr.Error(), v2Err.Error(), "divergent error (case %q, suffix %q)", tc.name, appTOMLSuffix)
			return
		}
		require.Equal(t, legacyCtx.Config, v2Ctx.Config, "Config diverges (case %q, suffix %q)", tc.name, appTOMLSuffix)
		require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(), "settings diverge (case %q, suffix %q)", tc.name, appTOMLSuffix)
	})
}
