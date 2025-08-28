package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetNonce(s.ctx, addr)
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	if s.logger != nil && s.logger.OnNonceChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		s.logger.OnNonceChangeV2(addr, s.GetNonce(addr), nonce, reason)
	}

	s.k.SetNonce(s.ctx, addr, nonce)
}
