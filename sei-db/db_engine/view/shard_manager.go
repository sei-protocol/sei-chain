package view

import (
	"errors"
	"hash/maphash"
	"sync"
)

var ErrNumShardsNotPowerOfTwo = errors.New("numShards must be a power of two and > 0")

// A utility for assigning keys to shard indices.
type shardManager struct {
	// A random seed that makes it hard for an attacker to predict the shard index and to skew the distribution.
	seed maphash.Seed
	// Used to perform a quick modulo operation to get the shard index (since numShards is a power of two)
	mask uint64
	// reusable Hash objects to avoid allocs
	pool sync.Pool
}

// Creates a new Sharder. Number of shards must be a power of two and greater than 0.
func newShardManager(numShards uint64) (*shardManager, error) {
	if numShards == 0 || (numShards&(numShards-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}

	return &shardManager{
		seed: maphash.MakeSeed(), // secret, randomized
		mask: numShards - 1,
		pool: sync.Pool{
			New: func() any { return new(maphash.Hash) },
		},
	}, nil
}

// Shard returns a shard index in [0, numShards).
// addr should be the raw address bytes (e.g., 20-byte ETH address).
func (s *shardManager) Shard(addr []byte) uint64 {
	h := s.pool.Get().(*maphash.Hash)
	h.SetSeed(s.seed)
	_, _ = h.Write(addr)
	x := h.Sum64()
	s.pool.Put(h)

	return x & s.mask
}

// ShardString is Shard for a key already held as a string. maphash.String is defined as
// Bytes(seed, []byte(addr)), and Shard's pooled Hash computes the same seeded sum, so a key lands in
// the same shard whichever form it arrives in.
func (s *shardManager) ShardString(addr string) uint64 {
	return maphash.String(s.seed, addr) & s.mask
}
