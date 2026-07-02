package evmonly

import (
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// StateReader supplies EVM-native state to an executor.
type StateReader interface {
	GetBalance(common.Address) *big.Int
	GetNonce(common.Address) uint64
	GetCode(common.Address) []byte
	GetState(common.Address, common.Hash) common.Hash
}

// StateWriter persists an executor-produced changeset.
type StateWriter interface {
	ApplyChangeSet(StateChangeSet)
}

// StateBackend is the minimal state boundary needed by the EVM-only executor.
type StateBackend interface {
	StateReader
	StateWriter
}

// MemoryState is a small EVM-native state backend for tests and early wiring.
type MemoryState struct {
	mu       sync.RWMutex
	accounts map[common.Address]*StateAccount
}

// StateAccount is an EVM-native account snapshot.
type StateAccount struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
	Storage map[common.Hash]common.Hash
}

func NewMemoryState() *MemoryState {
	return &MemoryState{accounts: map[common.Address]*StateAccount{}}
}

func (s *MemoryState) GetBalance(addr common.Address) *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if acct, ok := s.accounts[addr]; ok && acct.Balance != nil {
		return new(big.Int).Set(acct.Balance)
	}
	return new(big.Int)
}

func (s *MemoryState) SetBalance(addr common.Address, balance *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct := s.getOrCreateAccountLocked(addr)
	acct.Balance = cloneBig(balance)
}

func (s *MemoryState) GetNonce(addr common.Address) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if acct, ok := s.accounts[addr]; ok {
		return acct.Nonce
	}
	return 0
}

func (s *MemoryState) SetNonce(addr common.Address, nonce uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct := s.getOrCreateAccountLocked(addr)
	acct.Nonce = nonce
}

func (s *MemoryState) GetCode(addr common.Address) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if acct, ok := s.accounts[addr]; ok {
		return cloneBytes(acct.Code)
	}
	return nil
}

func (s *MemoryState) SetCode(addr common.Address, code []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct := s.getOrCreateAccountLocked(addr)
	acct.Code = cloneBytes(code)
}

func (s *MemoryState) GetState(addr common.Address, key common.Hash) common.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if acct, ok := s.accounts[addr]; ok && acct.Storage != nil {
		return acct.Storage[key]
	}
	return common.Hash{}
}

func (s *MemoryState) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct := s.getOrCreateAccountLocked(addr)
	if acct.Storage == nil {
		acct.Storage = map[common.Hash]common.Hash{}
	}
	if value == (common.Hash{}) {
		delete(acct.Storage, key)
		return
	}
	acct.Storage[key] = value
}

func (s *MemoryState) ApplyChangeSet(cs StateChangeSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, change := range cs.Balances {
		acct := s.getOrCreateAccountLocked(change.Address)
		acct.Balance = cloneBig(change.Balance)
	}
	for _, change := range cs.Nonces {
		acct := s.getOrCreateAccountLocked(change.Address)
		acct.Nonce = change.Nonce
	}
	for _, change := range cs.Code {
		acct := s.getOrCreateAccountLocked(change.Address)
		if change.Delete {
			acct.Code = nil
		} else {
			acct.Code = cloneBytes(change.Code)
		}
	}
	for _, addr := range cs.StorageClears {
		acct := s.getOrCreateAccountLocked(addr)
		acct.Storage = map[common.Hash]common.Hash{}
	}
	for _, change := range cs.Storage {
		acct := s.getOrCreateAccountLocked(change.Address)
		if acct.Storage == nil {
			acct.Storage = map[common.Hash]common.Hash{}
		}
		if change.Delete {
			delete(acct.Storage, change.Key)
		} else {
			acct.Storage[change.Key] = change.Value
		}
	}
}

func (s *MemoryState) getOrCreateAccountLocked(addr common.Address) *StateAccount {
	acct, ok := s.accounts[addr]
	if !ok {
		acct = &StateAccount{Balance: new(big.Int), Storage: map[common.Hash]common.Hash{}}
		s.accounts[addr] = acct
	}
	return acct
}

func cloneBig(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(v)
}

func cloneOptionalBig(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}

func cloneBytes(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	return append([]byte(nil), v...)
}
