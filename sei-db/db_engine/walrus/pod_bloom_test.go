package walrus

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBloomSizingHandlesDegenerateInputs(t *testing.T) {
	// A filter for no keys is still probeable, so an empty pod needs no special case anywhere.
	bits, hashes := bloomSizing(0, 0.01)
	require.Equal(t, uint64(1), bits)
	require.Equal(t, uint8(1), hashes)

	// A rate loose enough to want less than one hash still gets one.
	_, hashes = bloomSizing(1_000, 0.9)
	require.GreaterOrEqual(t, hashes, uint8(1))

	// Tighter rates cost more bits per key.
	loose, _ := bloomSizing(1_000_000, 0.1)
	tight, _ := bloomSizing(1_000_000, 0.001)
	require.Greater(t, tight, loose)
}

func TestBloomHasNoFalseNegativesAndHitsItsRate(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "bloom")

	const keyCount = 20_000
	const rate = 0.01
	const salt = uint64(0x5eed_1234_abcd_0001)

	keys := make([][]byte, 0, keyCount)
	hashes := make([]uint64, 0, keyCount)
	for index := 0; index < keyCount; index++ {
		key := []byte(fmt.Sprintf("present-key-%08d", index))
		keys = append(keys, key)
		hashes = append(hashes, podKeyHash(salt, key))
	}
	_, err := writePodBloom(path, hashes, rate, salt)
	require.NoError(t, err)

	filter, err := openPodBloom(path)
	require.NoError(t, err)

	// A bloom filter is allowed to be wrong in one direction only. Losing a key would make a walk skip the
	// pod holding it and answer from an older one.
	for _, key := range keys {
		require.True(t, filter.MayContain(key), "filter lost key %q", key)
	}

	positives := 0
	const trials = 100_000
	for index := 0; index < trials; index++ {
		if filter.MayContain([]byte(fmt.Sprintf("absent-key-%08d", index))) {
			positives++
		}
	}
	observed := float64(positives) / float64(trials)
	require.Less(t, observed, rate*3, "false positive rate %v is far above the %v it was sized for", observed, rate)
}

func TestPodKeyHashDependsOnTheSalt(t *testing.T) {
	// Two keys collide only under the salt of the pod holding them, which is what stops a key crafted to
	// collide with another from doing so everywhere it is written.
	key := []byte("0123456789abcdef")
	require.NotEqual(t, podKeyHash(1, key), podKeyHash(2, key))
	require.Equal(t, podKeyHash(7, key), podKeyHash(7, key))

	// Long keys take a different path through the hash than short ones. Both have to carry the salt, and
	// neither may collapse distinct keys of the same length.
	for _, length := range []int{0, 1, maxInlineHashKey - 1, maxInlineHashKey, maxInlineHashKey + 1, 4096} {
		first := bytes.Repeat([]byte{0xa5}, length)
		second := append(bytes.Repeat([]byte{0xa5}, length), 0x00)

		require.NotEqual(t, podKeyHash(1, first), podKeyHash(2, first),
			"a %d byte key ignores the salt", length)
		require.Equal(t, podKeyHash(3, first), podKeyHash(3, first),
			"a %d byte key hashes inconsistently", length)
		require.NotEqual(t, podKeyHash(3, first), podKeyHash(3, second),
			"a %d byte key hashes the same as a longer one", length)
	}
}

func TestPodKeyHashSpreadsKeysSharingALongPrefix(t *testing.T) {
	// Real keys share their leading bytes by construction: every storage slot of one contract does. The hash
	// index exists to stop that from being a run, so keys differing only in their tail must scatter.
	const keyCount = 4096
	const salt = uint64(0xfeed_face_dead_beef)

	buckets := map[uint32]int{}
	for index := 0; index < keyCount; index++ {
		key := append(bytes.Repeat([]byte{0x03}, 24), byte(index>>8), byte(index))
		buckets[indexHash(podKeyHash(salt, key))>>20]++
	}

	// With 4096 keys over 4096 buckets, an ordering that respected the shared prefix would put them all in
	// one. A hash puts a bounded share in the busiest.
	busiest := 0
	for _, count := range buckets {
		if count > busiest {
			busiest = count
		}
	}
	require.Less(t, busiest, keyCount/16, "keys sharing a prefix clustered into %d of %d", busiest, keyCount)
}
