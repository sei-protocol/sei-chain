package main

import (
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

type generatedState struct {
	mu       sync.RWMutex
	frozen   atomic.Bool
	balances map[common.Address]*big.Int
	nonces   map[common.Address]uint64
	code     map[common.Address][]byte
	storage  map[common.Address]map[common.Hash]common.Hash
}

var _ evmonly.StateReader = (*generatedState)(nil)

var frozenZeroBalance = new(big.Int)

func newGeneratedState() *generatedState {
	return &generatedState{
		balances: map[common.Address]*big.Int{},
		nonces:   map[common.Address]uint64{},
		code:     map[common.Address][]byte{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (s *generatedState) Freeze() {
	s.frozen.Store(true)
}

func (s *generatedState) AccountExists(addr common.Address) bool {
	if s.frozen.Load() {
		if _, ok := s.balances[addr]; ok {
			return true
		}
		if _, ok := s.nonces[addr]; ok {
			return true
		}
		if _, ok := s.code[addr]; ok {
			return true
		}
		_, ok := s.storage[addr]
		return ok
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.balances[addr]; ok {
		return true
	}
	if _, ok := s.nonces[addr]; ok {
		return true
	}
	if _, ok := s.code[addr]; ok {
		return true
	}
	_, ok := s.storage[addr]
	return ok
}

func (s *generatedState) GetBalance(addr common.Address) *big.Int {
	if s.frozen.Load() {
		// Frozen reads return shared, non-owned pointers; StateReader consumers
		// must copy before mutation.
		if balance, ok := s.balances[addr]; ok && balance != nil {
			return balance
		}
		return frozenZeroBalance
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if balance, ok := s.balances[addr]; ok && balance != nil {
		return new(big.Int).Set(balance)
	}
	return new(big.Int)
}

func (s *generatedState) SetBalance(addr common.Address, balance *big.Int) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	if balance == nil {
		s.balances[addr] = new(big.Int)
		return
	}
	s.balances[addr] = new(big.Int).Set(balance)
}

func (s *generatedState) GetNonce(addr common.Address) uint64 {
	if s.frozen.Load() {
		return s.nonces[addr]
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nonces[addr]
}

func (s *generatedState) SetNonce(addr common.Address, nonce uint64) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[addr] = nonce
}

func (s *generatedState) GetCode(addr common.Address) []byte {
	if s.frozen.Load() {
		return cloneBytes(s.code[addr])
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBytes(s.code[addr])
}

func (s *generatedState) SetCode(addr common.Address, code []byte) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code[addr] = cloneBytes(code)
}

func (s *generatedState) GetState(addr common.Address, key common.Hash) common.Hash {
	if s.frozen.Load() {
		if accountStorage, ok := s.storage[addr]; ok {
			return accountStorage[key]
		}
		return common.Hash{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if accountStorage, ok := s.storage[addr]; ok {
		return accountStorage[key]
	}
	return common.Hash{}
}

func (s *generatedState) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	accountStorage, ok := s.storage[addr]
	if !ok {
		accountStorage = map[common.Hash]common.Hash{}
		s.storage[addr] = accountStorage
	}
	if value == (common.Hash{}) {
		delete(accountStorage, key)
		return
	}
	accountStorage[key] = value
}

func (s *generatedState) requireMutable() {
	if s.frozen.Load() {
		panic("generated state is frozen")
	}
}

func cloneBytes(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	return append([]byte(nil), v...)
}
