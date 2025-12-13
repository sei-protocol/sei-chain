package cmd

import (
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	seiapp "github.com/sei-protocol/sei-chain/app"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
)

// WASMConfig defines configuration for the wasm module.
type WASMConfig struct {
	QueryGasLimit uint64 `mapstructure:"query_gas_limit"`
	LruSize       uint64 `mapstructure:"lru_size"`
}

// CustomAppConfig extends the Cosmos SDK's Config with custom fields
// This structure is used for generating app.toml with custom sections
type CustomAppConfig struct {
	srvconfig.Config

	StateCommit     seidbconfig.StateCommitConfig `mapstructure:"state-commit"`
	StateStore      seidbconfig.StateStoreConfig  `mapstructure:"state-store"`
	WASM            WASMConfig                    `mapstructure:"wasm"`
	EVM             evmrpcconfig.Config           `mapstructure:"evm"`
	ETHReplay       replay.Config                 `mapstructure:"eth_replay"`
	ETHBlockTest    blocktest.Config              `mapstructure:"eth_block_test"`
	EvmQuery        querier.Config                `mapstructure:"evm_query"`
	LightInvariance seiapp.LightInvarianceConfig  `mapstructure:"light_invariance"`
}

// NewCustomAppConfig creates a CustomAppConfig with the given base config and EVM config
func NewCustomAppConfig(baseConfig *srvconfig.Config, evmConfig evmrpcconfig.Config) CustomAppConfig {
	return CustomAppConfig{
		Config:      *baseConfig,
		StateCommit: seidbconfig.DefaultStateCommitConfig(),
		StateStore:  seidbconfig.DefaultStateStoreConfig(),
		WASM: WASMConfig{
			QueryGasLimit: 300000,
			LruSize:       1,
		},
		EVM:             evmConfig,
		ETHReplay:       replay.DefaultConfig,
		ETHBlockTest:    blocktest.DefaultConfig,
		EvmQuery:        querier.DefaultConfig,
		LightInvariance: seiapp.DefaultLightInvarianceConfig,
	}
}
