package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	profile, start := s.startGetterProfile("db_get_nonce")
	defer finishGetterProfile(profile, start, "db_get_nonce")
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.nonce[addr]; ok {
			if profile != nil {
				profile.AddCount("db_get_nonce_cache_hit_count", 1)
			}
			return cached
		}
	}
	nonce := s.k.GetNonce(s.ctx, addr)
	if s.cacheEnabled() {
		s.readCache.nonce[addr] = nonce
	}
	return nonce
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	if s.logger != nil && s.logger.OnNonceChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		s.logger.OnNonceChangeV2(addr, s.GetNonce(addr), nonce, reason)
	}

	s.k.SetNonce(s.ctx, addr, nonce)
	if s.cacheEnabled() {
		s.readCache.nonce[addr] = nonce
	}
}
