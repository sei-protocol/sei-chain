package server

import (
	"testing"

	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
)

func TestInjectTelemetryChainID(t *testing.T) {
	t.Run("appends chain id when missing", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{{"foo", "bar"}},
		}

		got := injectTelemetryChainID(cfg, "sei-test-1")

		require.Equal(t, [][]string{
			{"foo", "bar"},
			{"chain_id", "sei-test-1"},
		}, got.GlobalLabels)
	})

	t.Run("preserves existing chain id label", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{
				{"foo", "bar"},
				{"chain_id", "existing-chain"},
			},
		}

		got := injectTelemetryChainID(cfg, "sei-test-1")

		require.Equal(t, cfg.GlobalLabels, got.GlobalLabels)
	})

	t.Run("ignores empty chain id", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{{"foo", "bar"}},
		}

		got := injectTelemetryChainID(cfg, "")

		require.Equal(t, cfg.GlobalLabels, got.GlobalLabels)
	})
}

func TestLocalServicesEnabledForEVMOnly(t *testing.T) {
	appConfig := serverconfig.DefaultConfig()
	appConfig.API.Enable = false
	appConfig.GRPC.Enable = false
	appConfig.Ingress = true
	evmConfig := evmrpcconfig.DefaultConfig
	evmConfig.HTTPEnabled = true
	evmConfig.WSEnabled = true

	require.True(t, localServicesEnabled(*appConfig, evmConfig))

	appConfig.Ingress = false
	require.False(t, localServicesEnabled(*appConfig, evmConfig))
}

func TestConfigureIngressProfile(t *testing.T) {
	ctx := NewDefaultContext()
	ctx.Viper.Set(FlagIngress, true)

	require.NoError(t, configureIngressProfile(ctx))
	require.Equal(t, []string{"null"}, ctx.Config.TxIndex.Indexer)
	require.Equal(t, "tcp://127.0.0.1:26657", ctx.Config.RPC.ListenAddress)
	require.False(t, ctx.Viper.GetBool("api.enable"))
	require.False(t, ctx.Viper.GetBool("grpc.enable"))
	require.False(t, ctx.Viper.GetBool("state-store.ss-enable"))
	require.True(t, ctx.Viper.GetBool("state-commit.sc-ingress-profile"))
	require.True(t, ctx.Viper.GetBool("evm.http_enabled"))
	require.True(t, ctx.Viper.GetBool("evm.ws_enabled"))
	require.False(t, ctx.Viper.GetBool("evm.enable_simulation"))
	require.Equal(t, uint32(16), ctx.Viper.GetUint32("wasm.memory_cache_size"))
	require.False(t, ctx.Viper.GetBool("light_invariance.supply_enabled"))
	require.Equal(t, uint64(1000), ctx.Viper.GetUint64("min-retain-blocks"))
}

func TestConfigureIngressProfileRequiresFullMode(t *testing.T) {
	ctx := NewDefaultContext()
	ctx.Config.Mode = tmconfig.ModeValidator
	ctx.Viper.Set(FlagIngress, true)

	require.Error(t, configureIngressProfile(ctx))
}

func TestConfigureIngressProfileRejectsFreeze(t *testing.T) {
	ctx := NewDefaultContext()
	ctx.Viper.Set(FlagIngress, true)
	ctx.Viper.Set(FlagFreezeHeight, 10)

	require.Error(t, configureIngressProfile(ctx))
}

func TestConfigureIngressProfileRejectsGRPCOnly(t *testing.T) {
	ctx := NewDefaultContext()
	ctx.Viper.Set(FlagIngress, true)
	ctx.Viper.Set(flagGRPCOnly, true)

	require.Error(t, configureIngressProfile(ctx))
}
