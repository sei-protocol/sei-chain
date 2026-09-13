package gigasim

import (
	"encoding/binary"
	"math/bits"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"
)

// keccakBloomBits is how a real log bloom picks its bits. The benchmark's blooms are not built this
// way, and these tests hold the mixed ones to what the store sees of the difference.
func keccakBloomBits(value []byte) bloomBits {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(value)
	sum := hasher.Sum(nil)
	var picked bloomBits
	for i := 0; i < 6; i += 2 {
		picked[i/2] = (uint(sum[i])<<8)&2047 + uint(sum[i+1])
	}
	return picked
}

// bloomTestValue is a distinct value per seed. The seed is written in rather than folded into every
// byte, which wraps at 256 and would hand these tests far fewer values than they ask for.
func bloomTestValue(seed int) []byte {
	value := make([]byte, hashLen)
	binary.BigEndian.PutUint64(value, uint64(seed)) //nolint:gosec // seeds are small and non-negative
	for i := 8; i < len(value); i++ {
		value[i] = byte(i * 7)
	}
	return value
}

// TestBloomBitsForFillsABloomLikeKeccakDoes pins the property the store is measured on. The bits are
// not the ones a filter would look for, but a receipt's bloom has to occupy its 256 bytes the same
// way, so the count of bits set across a corpus has to match what keccak would have set.
func TestBloomBitsForFillsABloomLikeKeccakDoes(t *testing.T) {
	const values = 2048
	var mixedSet, keccakSet int
	for seed := range values {
		value := bloomTestValue(seed)

		var mixed, keccak ethtypes.Bloom
		setBits(&mixed, bloomBitsFor(value))
		setBits(&keccak, keccakBloomBits(value))

		mixedSet += countBloomBits(mixed)
		keccakSet += countBloomBits(keccak)
	}
	// Three bits per value either way, less whatever collides; the collision rates have to agree.
	require.InDelta(t, keccakSet, mixedSet, float64(keccakSet)*0.01,
		"a mixed bloom must fill to the same density as a keccak one, or the corpus compresses differently")
}

// TestBloomBitsForIsDeterministic pins that a rerun of the same seed produces the same corpus, which
// is what lets two runs of the benchmark be compared.
func TestBloomBitsForIsDeterministic(t *testing.T) {
	for seed := range 64 {
		value := bloomTestValue(seed)
		require.Equal(t, bloomBitsFor(value), bloomBitsFor(value))
	}
}

// TestBloomBitsForSeparatesValues pins that the bloom is not degenerate. Blooms that collapsed onto
// a few bit patterns would compress far better than real ones and flatter the store.
func TestBloomBitsForSeparatesValues(t *testing.T) {
	const values = 4096
	seen := make(map[bloomBits]struct{}, values)
	for seed := range values {
		seen[bloomBitsFor(bloomTestValue(seed))] = struct{}{}
	}
	require.Greater(t, len(seen), values*99/100, "distinct values must land on distinct bits")
}

// TestReceiptCacheReturnsWhatItCached pins that a contract resolved once reads back the same, since
// a receipt repeats its hex address in two fields.
func TestReceiptCacheReturnsWhatItCached(t *testing.T) {
	cache := newReceiptCache()
	address := make([]byte, keys.AddressLen)
	for i := range address {
		address[i] = byte(i)
	}
	first := cache.contract(address)
	require.Equal(t, bytesToHex(address), first.hex)
	require.Equal(t, bloomBitsFor(address), first.bits)
	require.Equal(t, first, cache.contract(address), "the cached value must match the resolved one")
}

func countBloomBits(bloom ethtypes.Bloom) int {
	total := 0
	for _, b := range bloom {
		total += bits.OnesCount8(b)
	}
	return total
}
