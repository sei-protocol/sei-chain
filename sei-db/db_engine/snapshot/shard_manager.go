package snapshot

import (
	"errors"
	"hash/maphash"
)

var ErrNumShardsNotPowerOfTwo = errors.New("numShards must be a power of two and > 0")

// A utility for assigning keys to shard indices.
type shardManager struct {
	// A random seed that makes it hard for an attacker to predict the shard index and to skew the distribution.
	seed maphash.Seed
	// Used to perform a quick modulo operation to get the shard index (since numShards is a power of two)
	mask uint64
	// The number of shards keys are assigned across.
	numShards uint64
}

// Creates a new Sharder. Number of shards must be a power of two and greater than 0.
func newShardManager(numShards uint64) (*shardManager, error) {
	if numShards == 0 || (numShards&(numShards-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}

	return &shardManager{
		seed:      maphash.MakeSeed(), // secret, randomized
		mask:      numShards - 1,
		numShards: numShards,
	}, nil
}

// Shard returns a shard index in [0, numShards).
// addr should be the raw address bytes (e.g., 20-byte ETH address).
func (s *shardManager) Shard(addr []byte) uint64 {
	// maphash.Bytes is defined as the seeded Write/Sum64 sequence over addr, so this picks the same
	// shard a Hash object would, with no object to allocate and pool per key.
	return maphash.Bytes(s.seed, addr) & s.mask
}

// ShardString is Shard for a key already held as a string. maphash.String is defined as
// Bytes(seed, []byte(addr)), so a key lands in the same shard whichever form it arrives in.
func (s *shardManager) ShardString(addr string) uint64 {
	return maphash.String(s.seed, addr) & s.mask
}
