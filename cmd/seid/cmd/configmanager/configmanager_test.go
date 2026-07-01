package configmanager

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
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
