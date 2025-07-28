package generator

import (
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/scenarios"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

type Generator interface {
	Generate() *types.LoadTx
	GenerateN(n int) []*types.LoadTx
}

type generator struct {
	accounts types.AccountPool
	txg      scenarios.TxGenerator
}

func NewGenerator(accounts types.AccountPool,
	txg scenarios.TxGenerator) Generator {
	return &generator{
		accounts: accounts,
		txg:      txg,
	}
}

func (g *generator) GenerateN(n int) []*types.LoadTx {
	result := make([]*types.LoadTx, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, g.Generate())
	}
	return result
}

func (g *generator) Generate() *types.LoadTx {
	sender := g.accounts.NextAccount()
	receiver := g.accounts.NextAccount()
	return g.txg.Generate(&types.TxScenario{
		Sender:   sender,
		Receiver: receiver.Address,
		Nonce:    sender.GetAndIncrementNonce(),
	})
}
