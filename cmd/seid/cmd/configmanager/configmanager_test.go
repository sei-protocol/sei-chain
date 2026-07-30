package configmanager

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	seiconfig "github.com/sei-protocol/sei-config"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestSelect covers the dispatch table: unset and "legacy" select the
// LegacyConfigManager, "v2" selects the SeiConfigManager, and any other
// value is a hard error (no silent fallback).
func TestSelect(t *testing.T) {
	cases := []struct {
		name    string
		val     string
		want    ConfigManager
		wantErr bool
	}{
		{name: "unset", val: "", want: LegacyConfigManager{}},
		{name: "legacy", val: "legacy", want: LegacyConfigManager{}},
		{name: "v2", val: "v2", want: SeiConfigManager{}},
		{name: "garbage", val: "v3", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := Select(func(string) string { return tc.val })
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.want, mgr)
		})
	}
}

// TestResolveHomeDir_Flag confirms resolveHomeDir reads the --home flag — the
// value v2 validates against must be the dir the re-entered handler reads. (Env
// precedence follows viper, mirrored from the legacy handler; the end-to-end
// env-driven case is exercised by TestConfigManagerLegacyVsV2Differential_EnvHome
// in the cmd package, which resolves the test-binary-basename prefix and asserts
// legacy/v2 parity.)
func TestResolveHomeDir_Flag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(flags.FlagHome, "", "")
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, "/tmp/seid-test-home"))

	got, err := resolveHomeDir(cmd)
	require.NoError(t, err)
	require.Equal(t, "/tmp/seid-test-home", got)
}

// TestResolveHomeDirEnvAndPrecedence covers the two cells the flag case above does
// not: the home arriving through the environment, and an explicit flag outranking it.
//
// The key is derived from the running binary's basename rather than spelled as
// SEID_HOME, because that is how both resolveHomeDir and the legacy handler build it.
// A change that hardcoded "seid" would still satisfy a literal-key test while
// silently ceasing to answer the environment under any other binary name, and the
// test binary is never named seid, so deriving the key is what makes this real.
//
// This asserts resolveHomeDir alone. That it agrees with the home the legacy handler
// reads is the lockstep property, and it is asserted end to end against the real
// handler in TestConfigManagerLegacyVsV2Differential_EnvHome.
func TestResolveHomeDirEnvAndPrecedence(t *testing.T) {
	prefix, err := configtest.ServerEnvPrefix()
	require.NoError(t, err)
	envKey := configtest.ServerEnvKey(prefix, flags.FlagHome)

	t.Run("the environment supplies the home", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(envKey, "/tmp/seid-env-home")

		cmd := &cobra.Command{}
		cmd.Flags().String(flags.FlagHome, "", "")

		got, err := resolveHomeDir(cmd)
		require.NoError(t, err)
		require.Equal(t, "/tmp/seid-env-home", got,
			"an unchanged flag default ranks below AutomaticEnv, so %s resolves the home", envKey)
	})

	t.Run("an explicit flag outranks the environment", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(envKey, "/tmp/seid-env-home")

		cmd := &cobra.Command{}
		cmd.Flags().String(flags.FlagHome, "", "")
		require.NoError(t, cmd.Flags().Set(flags.FlagHome, "/tmp/seid-flag-home"))

		got, err := resolveHomeDir(cmd)
		require.NoError(t, err)
		require.Equal(t, "/tmp/seid-flag-home", got,
			"a changed flag outranks the environment in viper's precedence")
	})
}

// TestReadConfigFromDirMissingIsErrNotExist pins the contract validateAdvisory's
// silent-skip depends on: a missing config file must yield an error that
// errors.Is(os.ErrNotExist) recognizes, so a fresh-home boot skips the advisory
// read quietly instead of logging a spurious warning. If sei-config ever swaps
// to a custom not-found error, this fails here rather than going noisy in prod.
func TestReadConfigFromDirMissingIsErrNotExist(t *testing.T) {
	_, err := seiconfig.ReadConfigFromDir(t.TempDir())
	require.ErrorIs(t, err, os.ErrNotExist)
}
