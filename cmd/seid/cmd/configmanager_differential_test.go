package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

// errStopPreRun aborts the command after the config-resolution PreRunE, before
// StartCmd's RunE tries to boot a node.
var errStopPreRun = errors.New("stop after prerun")

// runConfigManager runs mgr.Apply inside a StartCmd's PreRunE against homeDir
// supplied via the --home flag, using seid's real app-config template, and
// returns the populated server context (the two channels start.go/app.New
// consume). It mirrors the harness in sei-cosmos/server/util_test.go.
func runConfigManager(t *testing.T, mgr configmanager.ConfigManager, homeDir string) *server.Context {
	t.Helper()
	return execConfigManager(t, mgr, startCmdForHome(t, homeDir))
}

// startCmdForHome builds a StartCmd with --home set to homeDir.
func startCmdForHome(t *testing.T, homeDir string) *cobra.Command {
	t.Helper()
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, homeDir))
	return cmd
}

// runConfigManagerEnvHome is runConfigManager's twin that supplies homeDir
// through the environment instead of --home, exercising the
// SetEnvPrefix/AutomaticEnv/replacer machinery that resolveHomeDir mirrors from
// the legacy handler (the flag-driven path never touches it). The env prefix is
// path.Base(os.Executable()) — the test binary — derived identically by BOTH
// the legacy handler (sei-cosmos/server/util.go:152,162-164) and v2's
// resolveHomeDir, so both resolve the same home. viper's lookup key for "home"
// is ToUpper(prefix + "_" + "home") with the ".","-" -> "_" replacer applied.
func runConfigManagerEnvHome(t *testing.T, mgr configmanager.ConfigManager, homeDir string) *server.Context {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	envKey := strings.NewReplacer(".", "_", "-", "_").Replace(
		strings.ToUpper(path.Base(exe) + "_" + flags.FlagHome))
	t.Setenv(envKey, homeDir)

	// Leave --home unset: an unchanged flag default ranks below AutomaticEnv in
	// viper's precedence, so the env value is what resolves.
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	return execConfigManager(t, mgr, cmd)
}

// execConfigManager runs mgr.Apply on the happy path (Apply succeeds; boot is
// aborted with errStopPreRun) and returns the populated server context. The
// caller configures how home is supplied (flag vs env) on cmd beforehand.
func execConfigManager(t *testing.T, mgr configmanager.ConfigManager, cmd *cobra.Command) *server.Context {
	t.Helper()
	ctx, err := runManager(t, mgr, cmd)
	require.NoError(t, err)
	return ctx
}

// runManager runs mgr.Apply inside cmd's PreRunE and returns the populated
// server context and the error Apply returned. Apply is the only boot-refusing
// call, so on the happy path it returns nil and boot is aborted with
// errStopPreRun; on a real config error it returns that error and runManager
// surfaces it unchanged. Advisory diagnostics go to seilog (not cmd's stderr),
// so they are not captured here — the invariants under test are the returned
// context and error, not the log text. The caller sets home on cmd beforehand.
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

// appTOMLPath and cfgTOMLPath are the two files the legacy creator writes into a
// home and both managers then read.
func appTOMLPath(home string) string { return filepath.Join(home, "config", "app.toml") }
func cfgTOMLPath(home string) string { return filepath.Join(home, "config", "config.toml") }

// seedDefaultConfig generates a complete, realistic config (all Sei sections) by
// letting the legacy creator write into a fresh home, and returns that home.
func seedDefaultConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	_ = runConfigManager(t, configmanager.LegacyConfigManager{}, home)
	return home
}

// appendToFile appends s to the file at path (both managers must still agree on
// the result).
func appendToFile(t *testing.T, path, s string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, []byte(s)...), 0o600))
}

// prependToFile prepends s to the file at path.
func prependToFile(t *testing.T, path, s string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append([]byte(s), b...), 0o600))
}

// replaceInFile replaces oldStr with newStr in the file at path, asserting
// oldStr was present so a corpus mutation can never silently become a no-op.
func replaceInFile(t *testing.T, path, oldStr, newStr string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(b), oldStr, "replace target %q not found — fixture would be vacuous", oldStr)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(b), oldStr, newStr)), 0o600))
}

// corpusCase is one realistic on-disk config shape, applied to a freshly-seeded
// default home. It is the shared unit both the table-driven differential and the
// fuzz target consume, so the set of "interesting shapes" lives in one place.
type corpusCase struct {
	name   string
	mutate func(t *testing.T, home string)
}

// configCorpus is the single source of the config shapes the parity proof runs
// over. Each case mutates a default home in place; parity must hold for all of
// them because v2 re-enters the legacy reader regardless of what it read.
func configCorpus() []corpusCase {
	return []corpusCase{
		{"default", func(t *testing.T, home string) {}},
		{"leading-comments-and-blanks", func(t *testing.T, home string) {
			prependToFile(t, appTOMLPath(home), "# corpus: a leading comment\n\n")
		}},
		{"unknown-section", func(t *testing.T, home string) {
			appendToFile(t, appTOMLPath(home), "\n[sei-corpus-unknown]\nkey = \"value\"\n")
		}},
		{"quoted-scalar", func(t *testing.T, home string) {
			// A number written as a quoted string — the sei-config lenient-decode
			// case (#36). v2 reads it; the channels must still match legacy.
			appendToFile(t, appTOMLPath(home), "\n[sei-corpus-scalar]\ncount = \"100000\"\n")
		}},
		{"cosmos-only-write-mode", func(t *testing.T, home string) {
			// The version-skew class: a config carrying the deprecated
			// state-commit.sc-write-mode "cosmos_only". sei-config still accepts it
			// as valid, so v2 raises no diagnostic today; the point here is that
			// both managers read it identically (parity). It becomes a *caught*
			// case only once fatal validation + a sei-config deprecation rule land.
			replaceInFile(t, appTOMLPath(home), `sc-write-mode = "memiavl_only"`, `sc-write-mode = "cosmos_only"`)
		}},
	}
}

// TestConfigManagerLegacyVsV2Differential is the core safety property: the v2
// manager must produce the SAME consumed config as the legacy path. v2 reads
// the config (to validate it) and then re-enters the legacy reader on the
// operator's ORIGINAL files — it does not rewrite — so the two paths read the
// SAME home and any difference is a real divergence, not a path artifact.
//
// It compares parsed semantics:
//   - serverCtx.Config (the *tmcfg.Config the node runs on), and
//   - serverCtx.Viper.AllSettings() (the AppOptions every Sei section reads via
//     appOpts.Get), both at end-of-PersistentPreRunE and after the start.go
//     chain-id mutation.
//
// A realistic fixture (all 11 Sei sections) is generated by letting the legacy
// handler create the full default config once; both managers then read it.
func TestConfigManagerLegacyVsV2Differential(t *testing.T) {
	home := t.TempDir()

	// Generate a complete, realistic config via the legacy creator (fresh home).
	_ = runConfigManager(t, configmanager.LegacyConfigManager{}, home)

	// Both managers read the same populated home. v2 is passthrough (no rewrite),
	// so RootDir and every other path-derived field match by construction.
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

// TestConfigManagerLegacyVsV2Differential_EnvHome exercises the env-precedence
// half of resolveHomeDir's mirror of the legacy handler — the flag-driven
// differential above never touches SetEnvPrefix/AutomaticEnv. When home is
// supplied via the environment (not --home), v2 must resolve the SAME home the
// legacy handler does; otherwise v2 would advisorily validate one dir while the
// re-entered legacy reader boots on another — a silent drift the advisory
// design cannot surface (no error, no diagnostic). This pins the seam so a
// future change to the legacy env-resolution can't diverge undetected.
func TestConfigManagerLegacyVsV2Differential_EnvHome(t *testing.T) {
	home := t.TempDir()

	// Populate a complete realistic config in `home` via the fresh-home legacy
	// creator, driven entirely through the env var (no --home).
	_ = runConfigManagerEnvHome(t, configmanager.LegacyConfigManager{}, home)

	legacyCtx := runConfigManagerEnvHome(t, configmanager.LegacyConfigManager{}, home)
	v2Ctx := runConfigManagerEnvHome(t, configmanager.SeiConfigManager{}, home)

	// Non-vacuous guard: the env var actually drove resolution. If the key were
	// wrong, both would fall back to StartCmd's "/foobar" default (and the
	// legacy creator would fail writing under it) — this asserts the env path
	// resolved to the temp home, for both managers.
	require.Equal(t, home, v2Ctx.Viper.GetString(flags.FlagHome),
		"env-provided home did not drive v2 resolution")
	require.Equal(t, home, legacyCtx.Viper.GetString(flags.FlagHome),
		"env-provided home did not drive legacy resolution")

	require.Equal(t, legacyCtx.Config, v2Ctx.Config,
		"serverCtx.Config differs between legacy and v2 on the env-home path")
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings(),
		"serverCtx.Viper settings differ between legacy and v2 on the env-home path")
}

// TestConfigManagerLegacyVsV2Differential_Corpus widens the parity proof from
// the single default fixture to a corpus of realistic on-disk shapes. Parity is
// by construction (v2 re-enters the legacy reader), so any shape an operator
// could present must produce identical channels — including shapes that exercise
// sei-config's own reader (quoted scalars, unknown keys), whose advisory read
// still must not perturb what the node boots on.
func TestConfigManagerLegacyVsV2Differential_Corpus(t *testing.T) {
	for _, tc := range configCorpus() {
		t.Run(tc.name, func(t *testing.T) {
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

// TestConfigManagerV2AdvisoryNeverRefusesBoot pins the advisory invariant: on a
// valid config, v2 boots exactly as legacy does (Apply returns nil, both
// channels match), regardless of any diagnostics it prints. v2 adds
// observability, never a new boot outcome.
func TestConfigManagerV2AdvisoryNeverRefusesBoot(t *testing.T) {
	home := seedDefaultConfig(t)

	v2Ctx, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))
	require.NoError(t, v2Err, "advisory validation must never refuse boot on a valid config")

	legacyCtx := runConfigManager(t, configmanager.LegacyConfigManager{}, home)
	require.Equal(t, legacyCtx.Config, v2Ctx.Config)
	require.Equal(t, legacyCtx.Viper.AllSettings(), v2Ctx.Viper.AllSettings())
}

// TestConfigManagerV2AdvisoryReadErrorMatchesLegacy pins the other half of the
// invariant: when the config is unreadable, v2 must not mask the failure or
// invent a new one. It logs an advisory read error (via seilog), then re-enters
// the legacy handler and returns exactly the error legacy returns.
func TestConfigManagerV2AdvisoryReadErrorMatchesLegacy(t *testing.T) {
	home := seedDefaultConfig(t)
	require.NoError(t, os.WriteFile(cfgTOMLPath(home), []byte("this is ] not [ valid toml"), 0o600))

	_, legacyErr := runManager(t, configmanager.LegacyConfigManager{}, startCmdForHome(t, home))
	_, v2Err := runManager(t, configmanager.SeiConfigManager{}, startCmdForHome(t, home))

	require.Error(t, legacyErr, "corrupt config.toml should fail the legacy reader")
	require.Equal(t, legacyErr.Error(), v2Err.Error(),
		"v2 must return the same boot error as legacy, not mask or add one")
}

// FuzzConfigManagerLegacyVsV2Parity is the exhaustive form of the corpus: it
// crosses every corpus shape with an arbitrary appended app.toml suffix, and
// asserts legacy and v2 reach the same outcome — identical channels when both
// succeed, the identical error when both fail. Parity is by construction, so the
// fuzzer should never find a divergence. Under `go test` (no -fuzz) it runs the
// seed corpus (each shape × a few suffixes), a deterministic differential in CI;
// under -fuzz it explores suffixes against every shape.
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
		tc := corpus[corpusIdx%uint(len(corpus))]

		home := seedDefaultConfig(t)
		tc.mutate(t, home)
		appendToFile(t, appTOMLPath(home), appTOMLSuffix)

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
