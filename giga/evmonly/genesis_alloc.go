package evmonly

import (
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
)

// GenesisAccount is an account's balance, nonce, code and storage at a chain's
// initial height. A nil Balance credits nothing.
type GenesisAccount struct {
	Balance *uint256.Int
	Nonce   uint64
	Code    []byte
	Storage map[common.Hash]common.Hash
}

// genesisAlloc is the accounts set up at the start of block height. The zero
// value sets up nothing.
type genesisAlloc struct {
	height   uint64
	accounts []common.Address
	alloc    map[common.Address]GenesisAccount
}

func newGenesisAlloc(height uint64, alloc map[common.Address]GenesisAccount) genesisAlloc {
	return genesisAlloc{height: height, accounts: slices.SortedFunc(maps.Keys(alloc), common.Address.Cmp), alloc: maps.Clone(alloc)}
}

func (g genesisAlloc) appliesTo(height uint64) bool {
	return len(g.accounts) > 0 && height == g.height
}

// apply credits each account its balance and sets its nonce, code and storage,
// in address order and each account's storage in key order.
func (g genesisAlloc) apply(stateDB *nativeStateDB) {
	for _, addr := range g.accounts {
		account := g.alloc[addr]
		if account.Balance != nil && !account.Balance.IsZero() {
			stateDB.AddBalance(addr, account.Balance, tracing.BalanceIncreaseGenesisBalance)
		}
		if account.Nonce != 0 {
			stateDB.SetNonce(addr, account.Nonce, tracing.NonceChangeGenesis)
		}
		if len(account.Code) > 0 {
			stateDB.SetCode(addr, account.Code, tracing.CodeChangeGenesis)
		}
		for _, key := range slices.SortedFunc(maps.Keys(account.Storage), common.Hash.Cmp) {
			stateDB.SetState(addr, key, account.Storage[key])
		}
	}
}
