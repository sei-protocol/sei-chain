package state

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	bz := s.k.PrefixStore(s.ctx, types.NonceKeyPrefix).Get(addr[:])
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64) {
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, nonce)
	s.k.PrefixStore(s.ctx, types.NonceKeyPrefix).Set(addr[:], length)
}
