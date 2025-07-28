package config

import "math/big"

// LoadConfig stores the configuration for load-related settings.
type LoadConfig struct {
	ChainID    int64          `json:"chain_id,omitempty"`
	Endpoints  []string       `json:"endpoints"`
	Accounts   *AccountConfig `json:"accounts,omitempty"`
	Scenarios  []Scenario     `json:"scenarios,omitempty"`
	MockDeploy bool           `json:"mock_deploy,omitempty"`
}

// GetChainID returns the chain ID as a big.Int.
func (c *LoadConfig) GetChainID() *big.Int {
	return big.NewInt(c.ChainID)
}

// AccountConfig stores the configuration for account generation.
type AccountConfig struct {
	NewAccountRate float64 `json:"new_account_rate,omitempty"`
	Accounts       int     `json:"accounts,omitempty"`
}

// Scenario represents each scenario in the load configuration.
type Scenario struct {
	Name     string         `json:"name,omitempty"`
	Weight   int            `json:"weight,omitempty"`
	Accounts *AccountConfig `json:"accounts,omitempty"`
}
