package generator

import (
	"sync"

	"github.com/sei-protocol/sei-chain/seiload/config"
	"github.com/sei-protocol/sei-chain/seiload/generator/scenarios"
	"github.com/sei-protocol/sei-chain/seiload/types"
)

// PrewarmGenerator generates self-transfer transactions to prewarm account nonces
type PrewarmGenerator struct {
	accountPools    []types.AccountPool
	evmScenario     scenarios.TxGenerator
	currentPoolIdx  int
	finished        bool
	mu              sync.RWMutex
}

// NewPrewarmGenerator creates a new prewarm generator using all account pools from the main generator
func NewPrewarmGenerator(config *config.LoadConfig, mainGenerator Generator) *PrewarmGenerator {
	// Get all account pools from the main generator
	accountPools := mainGenerator.GetAccountPools()
	
	// Create EVMTransfer scenario for prewarming
	evmScenario := scenarios.NewEVMTransferScenario()
	
	// Deploy/initialize the scenario (EVMTransfer doesn't need actual deployment)
	deployerAccounts := types.GenerateAccounts(1)
	deployer := deployerAccounts[0]
	evmScenario.Deploy(config, deployer)
	
	return &PrewarmGenerator{
		accountPools: accountPools,
		evmScenario:  evmScenario,
		currentPoolIdx: 0,
		finished:     false,
	}
}

// Generate generates self-transfer transactions until all accounts are prewarmed
func (pg *PrewarmGenerator) Generate() (*types.LoadTx, bool) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	
	// Check if we're already finished
	if pg.finished || pg.currentPoolIdx >= len(pg.accountPools) {
		return nil, false
	}
	
	// Get current pool
	currentPool := pg.accountPools[pg.currentPoolIdx]
	account := currentPool.NextAccount()
	
	// If this account has nonce > 0, we've already prewarmed it (round-robin means we're done with this pool)
	if account.Nonce > 0 {
		// Move to next pool
		pg.currentPoolIdx++
		
		// Check if we've finished all pools
		if pg.currentPoolIdx >= len(pg.accountPools) {
			pg.finished = true
			return nil, false
		}
		
		// Get account from next pool
		currentPool = pg.accountPools[pg.currentPoolIdx]
		account = currentPool.NextAccount()
		
		// If this account also has nonce > 0, we're completely done
		if account.Nonce > 0 {
			pg.finished = true
			return nil, false
		}
	}
	
	// Create self-transfer transaction
	scenario := &types.TxScenario{
		Name:     "EVMTransfer",
		Sender:   account,
		Receiver: account.Address, // Send to self
		Nonce:    account.GetAndIncrementNonce(),
	}
	
	// Generate the transaction using EVMTransfer scenario
	return pg.evmScenario.Generate(scenario), true
}

// GenerateN generates n prewarming transactions
func (pg *PrewarmGenerator) GenerateN(n int) []*types.LoadTx {
	result := make([]*types.LoadTx, 0, n)
	for i := 0; i < n; i++ {
		if tx, ok := pg.Generate(); ok {
			result = append(result, tx)
		} else {
			break // Generator is done
		}
	}
	return result
}

// GetAccountPools returns all account pools used by this prewarm generator
func (pg *PrewarmGenerator) GetAccountPools() []types.AccountPool {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	
	// Return a copy to prevent external modification
	pools := make([]types.AccountPool, len(pg.accountPools))
	copy(pools, pg.accountPools)
	return pools
}
