package generator

import (
	"seiload/generator/scenarios"
	types2 "seiload/types"
)

type scenarioGenerator struct {
	accounts types2.AccountPool
	txg      scenarios.TxGenerator
}

func NewScenarioGenerator(accounts types2.AccountPool,
	txg scenarios.TxGenerator) Generator {
	return &scenarioGenerator{
		accounts: accounts,
		txg:      txg,
	}
}

func (g *scenarioGenerator) GenerateN(n int) []*types2.LoadTx {
	result := make([]*types2.LoadTx, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, g.Generate())
	}
	return result
}

func (g *scenarioGenerator) Generate() *types2.LoadTx {
	sender := g.accounts.NextAccount()
	receiver := g.accounts.NextAccount()
	return g.txg.Generate(&types2.TxScenario{
		Name:     g.txg.Name(),
		Sender:   sender,
		Receiver: receiver.Address,
		Nonce:    sender.GetAndIncrementNonce(),
	})
}
