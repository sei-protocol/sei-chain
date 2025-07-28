package generator

import (
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/scenarios"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

type scenarioGenerator struct {
	accounts types.AccountPool
	txg      scenarios.TxGenerator
}

func NewScenarioGenerator(accounts types.AccountPool,
	txg scenarios.TxGenerator) Generator {
	return &scenarioGenerator{
		accounts: accounts,
		txg:      txg,
	}
}

func (g *scenarioGenerator) GenerateN(n int) []*types.LoadTx {
	result := make([]*types.LoadTx, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, g.Generate())
	}
	return result
}

func (g *scenarioGenerator) Generate() *types.LoadTx {
	sender := g.accounts.NextAccount()
	receiver := g.accounts.NextAccount()
	return g.txg.Generate(&types.TxScenario{
		Name:     g.txg.Name(),
		Sender:   sender,
		Receiver: receiver.Address,
		Nonce:    sender.GetAndIncrementNonce(),
	})
}
