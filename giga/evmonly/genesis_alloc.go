package evmonly

import (
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
)

// genesisAlloc is the balance each account is credited at the start of block
// height. The zero value credits nothing.
type genesisAlloc struct {
	height   uint64
	accounts []common.Address
	balances map[common.Address]*uint256.Int
}

func newGenesisAlloc(height uint64, alloc map[common.Address]*uint256.Int) genesisAlloc {
	return genesisAlloc{height: height, accounts: slices.SortedFunc(maps.Keys(alloc), common.Address.Cmp), balances: maps.Clone(alloc)}
}

func (g genesisAlloc) appliesTo(height uint64) bool {
	return len(g.accounts) > 0 && height == g.height
}

func (g genesisAlloc) credit(stateDB *nativeStateDB) {
	for _, addr := range g.accounts {
		stateDB.AddBalance(addr, g.balances[addr], tracing.BalanceIncreaseGenesisBalance)
	}
}
