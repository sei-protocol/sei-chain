package benchmark

import (
	"encoding/json"
	"os"

	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator/scenarios"
)

// LoadConfig reads a sei-load config file or returns a default config.
// The chainID and seiChainID are always overridden with actual values from the running chain.
func LoadConfig(configPath string, evmChainID int64, seiChainID string) (*config.LoadConfig, error) {
	if configPath == "" {
		// Return default config (EVMTransfer scenario)
		return &config.LoadConfig{
			ChainID:    evmChainID,
			SeiChainID: seiChainID,
			MockDeploy: true, // We handle deployment in-process
			Accounts:   &config.AccountConfig{Accounts: 5000},
			Scenarios: []config.Scenario{{
				Name:   scenarios.EVMTransfer,
				Weight: 1,
			}},
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg config.LoadConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Override chain IDs with actual values from the running chain
	cfg.ChainID = evmChainID
	cfg.SeiChainID = seiChainID
	// Always use mock deploy since we handle deployment in-process
	cfg.MockDeploy = true

	return &cfg, nil
}
