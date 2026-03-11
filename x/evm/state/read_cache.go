package state

import (
	"bytes"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/evmrpc/traceprofile"
)

type storageCacheKey struct {
	address common.Address
	slot    common.Hash
}

type readCache struct {
	state          map[storageCacheKey]common.Hash
	committedState map[storageCacheKey]common.Hash
	nonce          map[common.Address]uint64
	codeHash       map[common.Address]common.Hash
	code           map[common.Address][]byte
	codeSize       map[common.Address]int
	balance        map[common.Address]uint256.Int
}

func newReadCache() *readCache {
	return &readCache{
		state:          make(map[storageCacheKey]common.Hash),
		committedState: make(map[storageCacheKey]common.Hash),
		nonce:          make(map[common.Address]uint64),
		codeHash:       make(map[common.Address]common.Hash),
		code:           make(map[common.Address][]byte),
		codeSize:       make(map[common.Address]int),
		balance:        make(map[common.Address]uint256.Int),
	}
}

func (c *readCache) clone() *readCache {
	if c == nil {
		return nil
	}
	cloned := newReadCache()
	for key, value := range c.state {
		cloned.state[key] = value
	}
	for key, value := range c.committedState {
		cloned.committedState[key] = value
	}
	for key, value := range c.nonce {
		cloned.nonce[key] = value
	}
	for key, value := range c.codeHash {
		cloned.codeHash[key] = value
	}
	for key, value := range c.code {
		cloned.code[key] = cloneBytes(value)
	}
	for key, value := range c.codeSize {
		cloned.codeSize[key] = value
	}
	for key, value := range c.balance {
		cloned.balance[key] = value
	}
	return cloned
}

func (c *readCache) clear() {
	if c == nil {
		return
	}
	c.state = make(map[storageCacheKey]common.Hash)
	c.committedState = make(map[storageCacheKey]common.Hash)
	c.nonce = make(map[common.Address]uint64)
	c.codeHash = make(map[common.Address]common.Hash)
	c.code = make(map[common.Address][]byte)
	c.codeSize = make(map[common.Address]int)
	c.balance = make(map[common.Address]uint256.Int)
}

func (c *readCache) clearCommittedState() {
	if c == nil {
		return
	}
	c.committedState = make(map[storageCacheKey]common.Hash)
}

func (c *readCache) invalidateAccount(address common.Address) {
	if c == nil {
		return
	}
	delete(c.nonce, address)
	delete(c.codeHash, address)
	delete(c.code, address)
	delete(c.codeSize, address)
	delete(c.balance, address)
	for key := range c.state {
		if key.address == address {
			delete(c.state, key)
		}
	}
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return bytes.Clone(value)
}

func cloneUint256(value uint256.Int) *uint256.Int {
	cloned := new(uint256.Int)
	cloned.Set(&value)
	return cloned
}

func uint256FromBig(value *big.Int) *uint256.Int {
	if value == nil {
		return uint256.NewInt(0)
	}
	res, overflow := uint256.FromBig(value)
	if overflow {
		panic("balance overflow")
	}
	if res == nil {
		return uint256.NewInt(0)
	}
	return res
}

func (s *DBImpl) cacheEnabled() bool {
	return s.readCache != nil && s.ctx.IsTracing()
}

func (s *DBImpl) committedCacheEnabled() bool {
	return s.readCache != nil && len(s.snapshottedCtxs) > 0 && s.snapshottedCtxs[0].IsTracing()
}

func (s *DBImpl) traceProfile() traceprofile.Recorder {
	if s == nil {
		return nil
	}
	return traceprofile.FromContext(s.ctx.Context())
}

func (s *DBImpl) startGetterProfile(name string) (traceprofile.Recorder, time.Time) {
	profile := s.traceProfile()
	if profile == nil {
		return nil, time.Time{}
	}
	profile.AddCount(name+"_count", 1)
	return profile, time.Now()
}

func finishGetterProfile(profile traceprofile.Recorder, start time.Time, name string) {
	if profile == nil {
		return
	}
	profile.AddDuration(name+"_total", time.Since(start))
}
