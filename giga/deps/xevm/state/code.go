package state

import (
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	return s.k.GetCodeHash(s.ctx, addr)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	return s.k.GetCode(s.ctx, addr)
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) []byte {
	oldCode := s.GetCode(addr)
	codeStore := s.k.PrefixStore(s.ctx, types.CodeKeyPrefix)
	prevCode := codeStore.Get(addr[:])
	prevCodeExists := prevCode != nil
	prevMapping := captureAddressMapping(s, addr)
	if s.logger != nil && s.logger.OnCodeChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		oldHash := s.GetCodeHash(addr)

		s.logger.OnCodeChange(addr, oldHash, oldCode, common.Hash(crypto.Keccak256(code)), code)
	}

	s.k.SetCode(s.ctx, addr, code)
	s.journal = append(s.journal, &codeChange{
		addr:           addr,
		prevCode:       slices.Clone(prevCode),
		prevCodeExists: prevCodeExists,
		prevMapping:    prevMapping,
	})
	return oldCode
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	return s.k.GetCodeSize(s.ctx, addr)
}
