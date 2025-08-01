package generator

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/seiload/config"
	"github.com/sei-protocol/sei-chain/seiload/generator/scenarios"
	"github.com/sei-protocol/sei-chain/seiload/types"
)

// Generator interface defines the contract for transaction generators
type Generator interface {
	Generate() (*types.LoadTx, bool) // Returns transaction and true if more available, nil/false when done
	GenerateN(n int) []*types.LoadTx
	GetAccountPools() []types.AccountPool
}

// scenarioInstance represents a scenario instance with its configuration
type scenarioInstance struct {
	Name     string
	Weight   int
	Scenario scenarios.TxGenerator
	Accounts types.AccountPool
	Deployed bool
}

// configBasedGenerator manages scenario creation and deployment from config
type configBasedGenerator struct {
	config         *config.LoadConfig
	instances      []*scenarioInstance
	deployer       *types.Account
	sharedAccounts types.AccountPool   // Shared account pool when using top-level config
	accountPools   []types.AccountPool // All account pools (shared + scenario-specific)
	mu             sync.RWMutex
}

// CreateScenarios creates scenario instances based on the configuration
// Each scenario entry in config creates a separate instance, even if same name
func (g *configBasedGenerator) createScenarios() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Create shared account pool if top-level account config exists
	if g.config.Accounts != nil {
		accounts := types.GenerateAccounts(g.config.Accounts.Accounts)
		g.sharedAccounts = types.NewAccountPool(&types.AccountConfig{
			Accounts:       accounts,
			NewAccountRate: g.config.Accounts.NewAccountRate,
		})
		g.accountPools = append(g.accountPools, g.sharedAccounts)
	}

	for i, scenarioCfg := range g.config.Scenarios {
		// Create scenario instance using factory
		scenario := scenarios.CreateScenario(scenarioCfg.Name)

		// Determine account pool to use
		var accountPool types.AccountPool
		if scenarioCfg.Accounts != nil {
			// Scenario defines its own account settings - create separate pool
			accountCount := scenarioCfg.Accounts.Accounts
			newAccountRate := scenarioCfg.Accounts.NewAccountRate

			accounts := types.GenerateAccounts(accountCount)
			accountPool = types.NewAccountPool(&types.AccountConfig{
				Accounts:       accounts,
				NewAccountRate: newAccountRate,
			})
			g.accountPools = append(g.accountPools, accountPool)
		} else if g.sharedAccounts != nil {
			// Use shared account pool from top-level config
			accountPool = g.sharedAccounts
		} else {
			return errors.New("no accounts config defined")
		}

		// Count how many times this scenario name appears in the config
		nameCount := 0
		nameIndex := 0
		for j, otherScenario := range g.config.Scenarios {
			if otherScenario.Name == scenarioCfg.Name {
				if j == i {
					nameIndex = nameCount
				}
				nameCount++
			}
		}

		name := scenarioCfg.Name
		if nameCount > 1 {
			name = fmt.Sprintf("%s_%d", name, nameIndex)
		}

		// Create scenario instance
		instance := &scenarioInstance{
			Name:     name,
			Weight:   scenarioCfg.Weight,
			Scenario: scenario,
			Accounts: accountPool,
			Deployed: false,
		}

		g.instances = append(g.instances, instance)
	}

	return nil
}

// mockDeployAll deploys all scenario instances that require deployment (for unit tests).
func (g *configBasedGenerator) mockDeployAll() error {
	for _, instance := range g.instances {
		addr := types.GenerateAccounts(1)[0].Address
		if err := instance.Scenario.Attach(g.config, addr); err != nil {
			return err
		}
		instance.Deployed = true
	}
	return nil
}

// DeployAll deploys all scenario instances that require deployment
func (g *configBasedGenerator) deployAll() error {
	if g.config.MockDeploy {
		return g.mockDeployAll()
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// Deploy sequentially to ensure proper nonce management
	for _, instance := range g.instances {
		// Deploy the scenario
		address := instance.Scenario.Deploy(g.config, g.deployer)
		instance.Deployed = true

		if address.Cmp(common.Address{}) != 0 {
			fmt.Printf("✅ Deployed %s at address: %s\n", instance.Name, address.Hex())
		}

		// Increment deployer nonce for next deployment
		g.deployer.GetAndIncrementNonce()
	}

	return nil
}

// createWeightedGenerator creates a weighted scenarioGenerator from deployed scenarios
func (g *configBasedGenerator) createWeightedGenerator() (Generator, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.instances) == 0 {
		return nil, fmt.Errorf("no scenario instances created")
	}

	// Check that all scenarios are deployed
	for _, instance := range g.instances {
		if !instance.Deployed {
			return nil, fmt.Errorf("scenario %s is not deployed", instance.Name)
		}
	}

	// Create weighted configurations
	var weightedConfigs []*WeightedCfg
	for _, instance := range g.instances {
		// Create a scenarioGenerator for this scenario instance
		gen := NewScenarioGenerator(instance.Accounts, instance.Scenario)

		// Add to weighted config with the specified weight
		weightedConfigs = append(weightedConfigs, WeightedConfig(instance.Weight, gen))
	}

	// Create and return the weighted scenarioGenerator
	return NewWeightedGenerator(weightedConfigs...), nil
}

// GetAccountPools returns all account pools managed by this generator
func (g *configBasedGenerator) GetAccountPools() []types.AccountPool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Return a copy of the slice to prevent external modification
	pools := make([]types.AccountPool, len(g.accountPools))
	copy(pools, g.accountPools)
	return pools
}

// NewConfigBasedGenerator is a convenience method that combines all steps
func NewConfigBasedGenerator(cfg *config.LoadConfig) (Generator, error) {
	generator := &configBasedGenerator{
		config:    cfg,
		instances: make([]*scenarioInstance, 0),
		deployer:  types.GenerateAccounts(1)[0],
	}

	// Step 1: Create scenarios
	if err := generator.createScenarios(); err != nil {
		return nil, fmt.Errorf("failed to create scenarios: %w", err)
	}

	// Step 2: Deploy all scenarios
	if err := generator.deployAll(); err != nil {
		return nil, fmt.Errorf("failed to deploy scenarios: %w", err)
	}

	// Step 3: Create weighted scenarioGenerator
	weightedGen, err := generator.createWeightedGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create weighted scenarioGenerator: %w", err)
	}

	return weightedGen, nil
}
