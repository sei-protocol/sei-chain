package evmonly

import (
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// BalanceReader returns an account's current EVM balance.
type BalanceReader interface {
	GetBalance(common.Address) *big.Int
}

// BalanceStore holds current EVM balances over an immutable initial balance
// source.
type BalanceStore interface {
	BalanceReader
	ApplyBalanceChanges([]BalanceChange)
}

// PlaceholderBalanceStore is a process-local BalanceStore for runtimes whose
// persistent state backend does not expose EVM balances.
type PlaceholderBalanceStore struct {
	mu       sync.RWMutex
	initial  BalanceReader
	balances map[common.Address]*big.Int
}

// NewPlaceholderBalanceStore returns a balance store backed by initial for
// accounts without an applied balance change.
func NewPlaceholderBalanceStore(initial BalanceReader) *PlaceholderBalanceStore {
	return &PlaceholderBalanceStore{
		initial:  initial,
		balances: make(map[common.Address]*big.Int),
	}
}

// GetBalance returns the latest applied balance or the account's initial
// balance when it has not changed.
func (s *PlaceholderBalanceStore) GetBalance(address common.Address) *big.Int {
	s.mu.RLock()
	balance, ok := s.balances[address]
	if ok {
		balance = cloneBig(balance)
	}
	s.mu.RUnlock()
	if ok {
		return balance
	}
	if s.initial == nil {
		return new(big.Int)
	}
	return cloneBig(s.initial.GetBalance(address))
}

// ApplyBalanceChanges installs the post-block balances in changes.
func (s *PlaceholderBalanceStore) ApplyBalanceChanges(changes []BalanceChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, change := range changes {
		s.balances[change.Address] = cloneBig(change.Balance)
	}
}
