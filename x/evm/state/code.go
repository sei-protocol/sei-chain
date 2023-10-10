package state

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	store := s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix)
	bz := store.Get(addr[:])
	if bz == nil {
		return common.Hash{}
	}
	return common.BytesToHash(bz)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	return s.k.PrefixStore(s.ctx, types.CodeKeyPrefix).Get(addr[:])
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) {
	s.k.PrefixStore(s.ctx, types.CodeKeyPrefix).Set(addr[:], code)
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(code)))
	s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix).Set(addr[:], length)
	h := crypto.Keccak256Hash(code)
	s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix).Set(addr[:], h[:])
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	bz := s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix).Get(addr[:])
	if bz == nil {
		return 0
	}
	return int(binary.BigEndian.Uint64(bz))
}
