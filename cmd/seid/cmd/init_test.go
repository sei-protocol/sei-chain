package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/genesis"
	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
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

// TestCheckConfigOverwrite verifies that init refuses to overwrite existing config unless --overwrite is set.
func TestCheckConfigOverwrite(t *testing.T) {
	t.Run("no config files", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config")
		require.NoError(t, os.MkdirAll(configPath, 0755))
		require.NoError(t, checkConfigOverwrite(configPath, false))
		require.NoError(t, checkConfigOverwrite(configPath, true))
	})

	t.Run("config.toml exists without overwrite", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config")
		require.NoError(t, os.MkdirAll(configPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "config.toml"), []byte("# test"), 0644))

		err := checkConfigOverwrite(configPath, false)
		require.ErrorContains(t, err, "configuration files already exist")
		require.ErrorContains(t, err, "--overwrite")
		require.NoError(t, checkConfigOverwrite(configPath, true))
	})

	t.Run("app.toml exists without overwrite", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config")
		require.NoError(t, os.MkdirAll(configPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "app.toml"), []byte("# test"), 0644))

		err := checkConfigOverwrite(configPath, false)
		require.ErrorContains(t, err, "configuration files already exist")
		require.NoError(t, checkConfigOverwrite(configPath, true))
	})

	t.Run("both exist with overwrite", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config")
		require.NoError(t, os.MkdirAll(configPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "config.toml"), []byte("# test"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(configPath, "app.toml"), []byte("# test"), 0644))
		require.NoError(t, checkConfigOverwrite(configPath, true))
	})
}

// TestLoadOrWriteGenesis_ExplicitConfigWins: existing genesis + no overwrite leaves file unchanged.
func TestLoadOrWriteGenesis_ExplicitConfigWins(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	const chainID = "atlantic-2"

	// a minimal valid genesis with matching chain_id
	existing := &types.GenesisDoc{ChainID: chainID, AppState: json.RawMessage(`{}`)}
	err := existing.SaveAs(genFile)
	require.NoError(t, err)
	origContent, err := os.ReadFile(genFile)
	require.NoError(t, err)

	encCfg := app.MakeEncodingConfig()
	genDoc, err := loadOrWriteGenesis(log.NewNopLogger(), genFile, chainID, false, app.ModuleBasics, encCfg.Marshaler)
	require.NoError(t, err)
	require.NotNil(t, genDoc)
	require.Equal(t, chainID, genDoc.ChainID)

	// File must be unchanged
	afterContent, err := os.ReadFile(genFile)
	require.NoError(t, err)
	require.Equal(t, string(origContent), string(afterContent), "genesis file should not be overwritten")
}

// TestLoadOrWriteGenesis_WrongChainID: wrong chain_id in file returns error.
func TestLoadOrWriteGenesis_WrongChainID(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	existing := &types.GenesisDoc{ChainID: "wrong-chain", AppState: json.RawMessage(`{}`)}
	err := existing.SaveAs(genFile)
	require.NoError(t, err)

	encCfg := app.MakeEncodingConfig()
	_, err = loadOrWriteGenesis(log.NewNopLogger(), genFile, "atlantic-2", false, app.ModuleBasics, encCfg.Marshaler)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chain_id")
}

// TestLoadOrWriteGenesis_PathIsDirectory: path is directory returns error.
func TestLoadOrWriteGenesis_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	require.NoError(t, os.MkdirAll(genFile, 0755))

	encCfg := app.MakeEncodingConfig()
	_, err := loadOrWriteGenesis(log.NewNopLogger(), genFile, "atlantic-2", false, app.ModuleBasics, encCfg.Marshaler)
	require.Error(t, err)
	require.Contains(t, err.Error(), "directory")
}

// TestLoadOrWriteGenesis_WellKnownWritesEmbedded: well-known chain gets embedded genesis.
func TestLoadOrWriteGenesis_WellKnownWritesEmbedded(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	const chainID = "atlantic-2"
	require.True(t, genesis.IsWellKnown(chainID), "atlantic-2 should be well-known")
	encCfg := app.MakeEncodingConfig()

	genDoc, err := loadOrWriteGenesis(log.NewNopLogger(), genFile, chainID, false, app.ModuleBasics, encCfg.Marshaler)
	require.NoError(t, err)
	require.NotNil(t, genDoc)
	require.Equal(t, chainID, genDoc.ChainID)
	require.NotEmpty(t, genDoc.AppState)

	embedded, err := genesis.EmbeddedGenesisDoc(chainID)
	require.NoError(t, err)
	require.Equal(t, embedded.ChainID, genDoc.ChainID)
}

// TestLoadOrWriteGenesis_OverwriteReplacesFile: overwrite=true replaces existing file.
func TestLoadOrWriteGenesis_OverwriteReplacesFile(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	const chainID = "atlantic-2"
	existing := &types.GenesisDoc{ChainID: chainID, AppState: json.RawMessage(`{"old":true}`)}
	require.NoError(t, existing.SaveAs(genFile))

	encCfg := app.MakeEncodingConfig()
	genDoc, err := loadOrWriteGenesis(log.NewNopLogger(), genFile, chainID, true, app.ModuleBasics, encCfg.Marshaler)
	require.NoError(t, err)
	require.NotNil(t, genDoc)
	// Should be overwritten
	require.NotEqual(t, `{"old":true}`, string(genDoc.AppState))
}

// TestLoadOrWriteGenesis_UnknownChainWritesDefault: unknown chain gets default genesis.
func TestLoadOrWriteGenesis_UnknownChainWritesDefault(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "genesis.json")
	const chainID = "custom-chain-1"
	require.False(t, genesis.IsWellKnown(chainID), "custom-chain-1 should not be well-known")

	encCfg := app.MakeEncodingConfig()
	genDoc, err := loadOrWriteGenesis(log.NewNopLogger(), genFile, chainID, false, app.ModuleBasics, encCfg.Marshaler)
	require.NoError(t, err)
	require.NotNil(t, genDoc)
	require.Equal(t, chainID, genDoc.ChainID)
	require.NotEmpty(t, genDoc.AppState)

	// Should contain known module keys
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(genDoc.AppState, &state))
	require.Contains(t, state, "bank")
	require.Contains(t, state, "staking")
	require.Contains(t, state, "genutil")
}
