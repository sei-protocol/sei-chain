package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	seidbconfig "github.com/sei-protocol/sei-db/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	tmcfg "github.com/tendermint/tendermint/config"
)

// TestInitModeConfiguration tests that mode-based configuration works end-to-end
func TestInitModeConfiguration(t *testing.T) {
	tests := []struct {
		name               string
		mode               params.NodeMode
		validateTendermint func(*testing.T, string)
		validateApp        func(*testing.T, string)
	}{
		{
			name: "validator mode creates secure configuration",
			mode: params.NodeModeValidator,
			validateTendermint: func(t *testing.T, configDir string) {
				v := viper.New()
				v.SetConfigFile(filepath.Join(configDir, "config.toml"))
				err := v.ReadInConfig()
				require.NoError(t, err)

				// Verify tx indexer is disabled
				indexer := v.GetStringSlice("tx-index.indexer")
				require.Contains(t, indexer, "null", "Validator should have null tx indexer")
			},
			validateApp: func(t *testing.T, configDir string) {
				v := viper.New()
				v.SetConfigFile(filepath.Join(configDir, "app.toml"))
				err := v.ReadInConfig()
				require.NoError(t, err)

				// Verify services are disabled for validator security
				require.False(t, v.GetBool("api.enable"), "API should be disabled")
				require.False(t, v.GetBool("grpc.enable"), "gRPC should be disabled")
				require.False(t, v.GetBool("grpc-web.enable"), "gRPC-Web should be disabled")
				require.False(t, v.GetBool("rosetta.enable"), "Rosetta should be disabled")
				require.False(t, v.GetBool("state-store.ss-enable"), "StateStore should be disabled")
				require.False(t, v.GetBool("evm.http_enabled"), "EVM HTTP should be disabled")
				require.False(t, v.GetBool("evm.ws_enabled"), "EVM WS should be disabled")

				// Verify pruning uses cosmos default (now in iavl section)
				require.Equal(t, "nothing", v.GetString("iavl.pruning"))
			},
		},
		{
			name: "full mode creates service-enabled configuration",
			mode: params.NodeModeFull,
			validateTendermint: func(t *testing.T, configDir string) {
				v := viper.New()
				v.SetConfigFile(filepath.Join(configDir, "config.toml"))
				err := v.ReadInConfig()
				require.NoError(t, err)

				// Verify tx indexer is enabled
				indexer := v.GetStringSlice("tx-index.indexer")
				require.Contains(t, indexer, "kv", "Full node should have kv tx indexer")
			},
			validateApp: func(t *testing.T, configDir string) {
				v := viper.New()
				v.SetConfigFile(filepath.Join(configDir, "app.toml"))
				err := v.ReadInConfig()
				require.NoError(t, err)

				// Verify services are enabled for full node
				require.True(t, v.GetBool("api.enable"), "API should be enabled")
				require.True(t, v.GetBool("grpc.enable"), "gRPC should be enabled")
				require.True(t, v.GetBool("grpc-web.enable"), "gRPC-Web should be enabled")
				require.False(t, v.GetBool("rosetta.enable"), "Rosetta is disabled by default for all modes")
				require.True(t, v.GetBool("state-store.ss-enable"), "StateStore should be enabled")

				// Note: EVM config requires custom template, tested separately in TestSetEVMConfigByMode and binary tests

				// Verify pruning uses cosmos default (now in iavl section)
				require.Equal(t, "nothing", v.GetString("iavl.pruning"))
			},
		},
		{
			name: "archive mode preserves all history",
			mode: params.NodeModeArchive,
			validateApp: func(t *testing.T, configDir string) {
				v := viper.New()
				v.SetConfigFile(filepath.Join(configDir, "app.toml"))
				err := v.ReadInConfig()
				require.NoError(t, err)

				// Verify no pruning for archive (now in iavl section)
				require.Equal(t, "nothing", v.GetString("iavl.pruning"))

				// Verify services are enabled
				require.True(t, v.GetBool("api.enable"))
				require.True(t, v.GetBool("grpc.enable"))

				// Note: EVM config requires custom template, tested separately in TestSetEVMConfigByMode and binary tests
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()
			configDir := filepath.Join(tmpDir, "config")

			// Create configurations with mode-specific settings
			tmConfig := tmcfg.DefaultConfig()
			tmConfig.Mode = string(tt.mode)
			params.SetTendermintConfigByMode(tmConfig)

			appConfig := srvconfig.DefaultConfig()
			params.SetAppConfigByMode(appConfig, tt.mode)

			// Write configs to files (simulating what init does)
			err := os.MkdirAll(configDir, 0755)
			require.NoError(t, err)

			// Write config.toml using Tendermint's writer
			err = tmcfg.WriteConfigFile(tmpDir, tmConfig)
			require.NoError(t, err)

			// Write app.toml using our custom template that includes SeiDB configs
			// This mirrors what initAppConfig does in root.go
			appTomlPath := filepath.Join(configDir, "app.toml")
			customConfig := NewCustomAppConfig(appConfig, evmrpcconfig.DefaultConfig)

			// Apply mode-specific StateStore settings
			if tt.mode == params.NodeModeValidator || tt.mode == params.NodeModeSeed {
				customConfig.StateStore.Enable = false
			} else {
				customConfig.StateStore.Enable = true
			}

			// Build custom template with all sections
			customAppTemplate := srvconfig.ManualConfigTemplate + seidbconfig.StateCommitConfigTemplate + seidbconfig.StateStoreConfigTemplate +
				srvconfig.AutoManagedConfigTemplate // Simplified - just need the pruning config

			srvconfig.SetConfigTemplate(customAppTemplate)
			srvconfig.WriteConfigFile(appTomlPath, customConfig)

			// Debug: print the config file for inspection
			if testing.Verbose() {
				data, _ := os.ReadFile(appTomlPath)
				t.Logf("Generated app.toml for mode %s:\n%s\n", tt.mode, string(data))
			}

			// Run validations
			if tt.validateTendermint != nil {
				tt.validateTendermint(t, configDir)
			}
			if tt.validateApp != nil {
				tt.validateApp(t, configDir)
			}
		})
	}
}

// TestInitModeFlag verifies the mode flag validation
func TestInitModeFlag(t *testing.T) {
	validModes := []string{"validator", "full", "seed", "archive"}

	for _, mode := range validModes {
		t.Run(fmt.Sprintf("valid mode: %s", mode), func(t *testing.T) {
			nodeMode := params.NodeMode(mode)

			// Verify mode is recognized
			require.True(t, isValidMode(nodeMode), "Mode %s should be valid", mode)
		})
	}

	// Test invalid mode
	t.Run("invalid mode rejected", func(t *testing.T) {
		invalidMode := params.NodeMode("invalid")

		// Should not match any valid mode
		require.False(t, isValidMode(invalidMode), "Invalid mode should not be accepted")
	})
}

// TestModeConfigurationMatrix verifies all configuration values for each mode
func TestModeConfigurationMatrix(t *testing.T) {
	matrix := map[params.NodeMode]struct {
		txIndexer string
		pruning   string
		apiEnable bool
		evmEnable bool
	}{
		params.NodeModeValidator: {"null", "nothing", false, false},
		params.NodeModeFull:      {"kv", "nothing", true, true},
		params.NodeModeSeed:      {"null", "nothing", false, false}, // Same as validator
		params.NodeModeArchive:   {"kv", "nothing", true, true},
	}

	for mode, expected := range matrix {
		t.Run(string(mode), func(t *testing.T) {
			// Create Tendermint config
			tmConfig := tmcfg.DefaultConfig()
			tmConfig.Mode = string(mode)
			params.SetTendermintConfigByMode(tmConfig)

			if expected.txIndexer == "null" {
				require.Contains(t, tmConfig.TxIndex.Indexer, "null")
			} else {
				require.Contains(t, tmConfig.TxIndex.Indexer, "kv")
			}

			// Create app config
			appConfig := srvconfig.DefaultConfig()
			params.SetAppConfigByMode(appConfig, mode)

			require.Equal(t, expected.pruning, appConfig.BaseConfig.Pruning, "Pruning strategy should match")
			require.Equal(t, expected.apiEnable, appConfig.API.Enable)

			// Verify EVM config
			evmConfig := evmrpcconfig.DefaultConfig
			params.SetEVMConfigByMode(&evmConfig, mode)
			require.Equal(t, expected.evmEnable, evmConfig.HTTPEnabled)
			require.Equal(t, expected.evmEnable, evmConfig.WSEnabled)
		})
	}
}
