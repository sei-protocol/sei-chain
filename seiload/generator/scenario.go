package generator

import (
	"sync"

	"github.com/sei-protocol/sei-chain/seiload/generator/scenarios"
	"github.com/sei-protocol/sei-chain/seiload/types"
)

type scenarioGenerator struct {
	scenario    scenarios.TxGenerator
	accountPool types.AccountPool
	mu          sync.RWMutex
}

func NewScenarioGenerator(accounts types.AccountPool,
	txg scenarios.TxGenerator) Generator {
	return &scenarioGenerator{
		scenario:    txg,
		accountPool: accounts,
	}
}

func (g *scenarioGenerator) GenerateN(n int) []*types.LoadTx {
	result := make([]*types.LoadTx, 0, n)
	for i := 0; i < n; i++ {
		if tx, ok := g.Generate(); ok {
			result = append(result, tx)
		} else {
			break // Generator is done
		}
	}
	return result
}

func (g *scenarioGenerator) Generate() (*types.LoadTx, bool) {
	sender := g.accountPool.NextAccount()
	receiver := g.accountPool.NextAccount()
	return g.scenario.Generate(&types.TxScenario{
		Name:     g.scenario.Name(),
		Sender:   sender,
		Receiver: receiver.Address,
		Nonce:    sender.GetAndIncrementNonce(),
	}), true
}

func (sg *scenarioGenerator) GetAccountPools() []types.AccountPool {
	sg.mu.RLock()
	defer sg.mu.RUnlock()
	return []types.AccountPool{sg.accountPool}
}
