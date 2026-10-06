package cmd

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// pprofProfileRateKeys are the config.toml keys that set the mutex and block profile rates.
var pprofProfileRateKeys = []string{"rpc.pprof-mutex-profile-fraction", "rpc.pprof-block-profile-rate"}

// configManagers are the managers seid selects between at boot.
var configManagers = map[string]configmanager.ConfigManager{
	"legacy": configmanager.LegacyConfigManager{},
	"v2":     configmanager.SeiConfigManager{},
}

// TestPprofProfileRatesResolveFromTheRPCSection boots seid's configuration path on a
// config.toml that sets both rates under [rpc], and checks the node's RPC config holds them.
func TestPprofProfileRatesResolveFromTheRPCSection(t *testing.T) {
	const body = `
[rpc]
pprof-laddr = "localhost:6060"
pprof-mutex-profile-fraction = 100
pprof-block-profile-rate = 1000000
`
	for name, mgr := range configManagers {
		t.Run(name, func(t *testing.T) {
			configtest.Isolate(t)
			home := configtest.NewHome(t)
			home.WriteConfigTOML(t, []byte(body))

			ctx := runConfigManager(t, mgr, home)
			require.Equal(t, 100, ctx.Config.RPC.PprofMutexProfileFraction)
			require.Equal(t, 1000000, ctx.Config.RPC.PprofBlockProfileRate)
		})
	}
}

// TestPprofProfileRatesAreOffInTheGeneratedConfig boots seid on an empty home, and checks
// the config.toml the boot writes carries both rate keys under [rpc] at 0.
func TestPprofProfileRatesAreOffInTheGeneratedConfig(t *testing.T) {
	for name, mgr := range configManagers {
		t.Run(name, func(t *testing.T) {
			configtest.Isolate(t)
			home := configtest.NewHome(t)

			ctx := runConfigManager(t, mgr, home)
			require.Zero(t, ctx.Config.RPC.PprofMutexProfileFraction)
			require.Zero(t, ctx.Config.RPC.PprofBlockProfileRate)

			generated := viper.New()
			generated.SetConfigFile(home.ConfigTOMLPath())
			require.NoError(t, generated.ReadInConfig())
			for _, key := range pprofProfileRateKeys {
				require.True(t, generated.IsSet(key), "generated config.toml is missing %s", key)
				require.Zero(t, generated.GetInt(key), key)
			}
		})
	}
}
