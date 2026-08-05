package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeHash(s.ctx, addr)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	// nil codeCache means caching is disabled (simulation/RPC/trace/wasmd-entry); never allocate here.
	if s.codeCache != nil {
		if code, ok := s.codeCache[addr]; ok {
			return code
		}
	}
	code := s.k.GetCode(s.ctx, addr)
	if s.codeCache == nil {
		return code
	}
	// Keep empty code as nil to match Keeper.GetCode, and store a copy so the
	// memo is not aliased to the keeper/store-backed slice.
	if len(code) == 0 {
		s.codeCache[addr] = nil
		return nil
	}
	s.putCodeCache(addr, code)
	return s.codeCache[addr]
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	oldCode := s.GetCode(addr)
	if s.logger != nil && s.logger.OnCodeChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		oldHash := s.GetCodeHash(addr)

		s.logger.OnCodeChange(addr, oldHash, oldCode, crypto.Keccak256Hash(code), code)
	}

	s.k.SetCode(s.ctx, addr, code)
	s.journalCodeCacheMutation(addr)
	s.putCodeCache(addr, code)
	return oldCode
}

// RefreshCodeCache updates the deliver-tx code memo after a keeper store write
// that bypassed SetCode (so gas can be charged against a different ctx meter).
func (s *DBImpl) RefreshCodeCache(addr common.Address, code []byte) {
	s.journalCodeCacheMutation(addr)
	s.putCodeCache(addr, code)
}

// journalCodeCacheMutation records the prior memo entry so RevertToSnapshot can
// restore this address only. Call before putCodeCache / delete on mutation paths;
// pure GetCode fills must not journal (nested reverts should keep those warms).
func (s *DBImpl) journalCodeCacheMutation(addr common.Address) {
	if s.codeCache == nil {
		return
	}
	prev, had := s.codeCache[addr]
	s.journal = append(s.journal, &codeCacheChange{account: addr, prev: prev, had: had})
}

func (s *DBImpl) putCodeCache(addr common.Address, code []byte) {
	if s.codeCache == nil {
		return
	}
	if len(code) == 0 {
		s.codeCache[addr] = nil
		return
	}
	cached := make([]byte, len(code))
	copy(cached, code)
	s.codeCache[addr] = cached
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeSize(s.ctx, addr)
}
