package evmonly

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
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

// ProtocolAccounts returns the contracts every EVM-only chain holds from its
// initial height: the EIP-2935 history storage contract.
func ProtocolAccounts() map[common.Address]GenesisAccount {
	return map[common.Address]GenesisAccount{
		params.HistoryStorageAddress: {Nonce: 1, Code: slices.Clone(params.HistoryStorageCode)},
	}
}

// newGenesisAlloc returns alloc and the protocol accounts, set up at the start
// of block height. It panics when alloc sets a protocol account.
func newGenesisAlloc(height uint64, alloc map[common.Address]GenesisAccount) genesisAlloc {
	all := ProtocolAccounts()
	for addr, account := range alloc {
		if _, ok := all[addr]; ok {
			panic(fmt.Sprintf("genesis alloc sets protocol account %s", addr))
		}
		all[addr] = account
	}
	return genesisAlloc{height: height, accounts: slices.SortedFunc(maps.Keys(all), common.Address.Cmp), alloc: all}
}

func (g genesisAlloc) appliesTo(height uint64) bool {
	return len(g.accounts) > 0 && height == g.height
}

// creditsBalance reports whether any account has a nonzero Balance.
func (g genesisAlloc) creditsBalance() bool {
	for _, account := range g.alloc {
		if account.Balance != nil && !account.Balance.IsZero() {
			return true
		}
	}
	return false
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
