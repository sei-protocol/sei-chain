package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	return s.k.GetNonce(s.ctx, addr)
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	prevNonce := s.GetNonce(addr)
	prevExists := s.k.PrefixStore(s.ctx, types.NonceKeyPrefix).Has(addr[:])
	if s.logger != nil && s.logger.OnNonceChangeV2 != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		s.logger.OnNonceChangeV2(addr, prevNonce, nonce, reason)
	}

	s.k.SetNonce(s.ctx, addr, nonce)
	s.journal = append(s.journal, &nonceChange{addr: addr, prev: prevNonce, prevExists: prevExists})
}
