package generator_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/seiload/config"
	"github.com/sei-protocol/sei-chain/seiload/generator"
	"github.com/sei-protocol/sei-chain/seiload/generator/scenarios"
)

func TestScenarioWeightsAndAccountDistribution(t *testing.T) {
	cfg := &config.LoadConfig{
		ChainID:    7777,
		MockDeploy: true,
		Scenarios: []config.Scenario{
			{
				Name:   scenarios.ERC20,
				Weight: 2,
				Accounts: &config.AccountConfig{
					Accounts:       10,
					NewAccountRate: 0.0,
				},
			},
			{
				Name:   scenarios.EVMTransfer,
				Weight: 3,
				Accounts: &config.AccountConfig{
					Accounts:       20,
					NewAccountRate: 0.0,
				},
			},
		},
	}

	gen, err := generator.NewConfigBasedGenerator(cfg)
	require.NoError(t, err)
	require.NotNil(t, gen)

	totalTxs := 100
	txs := gen.GenerateN(totalTxs)
	require.Len(t, txs, totalTxs)

	// Count occurrences per scenario
	scenarioCounts := make(map[string]int)
	for _, tx := range txs {
		require.NotNil(t, tx.Scenario)
		scenario := tx.Scenario.Name
		scenarioCounts[scenario]++
	}

	// Weight 2:3 → Expect ≈40:60 distribution (±10 allowed)
	require.InDelta(t, 40, float64(scenarioCounts["ERC20"]), 10)
	require.InDelta(t, 60, float64(scenarioCounts["EVMTransfer"]), 10)
}
