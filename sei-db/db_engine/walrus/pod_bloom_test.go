package walrus

import (
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
	path := filepath.Join(directory, "test.pod.bloom")

	const keyCount = 20_000
	const rate = 0.01

	keys := make([][]byte, 0, keyCount)
	for index := 0; index < keyCount; index++ {
		keys = append(keys, []byte(fmt.Sprintf("present-key-%08d", index)))
	}
	_, err := writePodBloom(path, keys, rate)
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

func TestKeyPrefixPreservesKeyOrder(t *testing.T) {
	// Level one is searched on the prefix alone, so the prefix has to order keys the same way the keys order
	// themselves, including when one key is a prefix of another.
	ordered := [][]byte{
		{},
		{0x00},
		{0x00, 0x01},
		{0x01},
		{0x01, 0x00},
		{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		{0x03, 0xff},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}
	for index := 1; index < len(ordered); index++ {
		previous := keyPrefix(ordered[index-1])
		current := keyPrefix(ordered[index])
		require.LessOrEqual(t, previous, current, "prefix order disagrees at index %d", index)
	}

	// Keys that differ only past the eighth byte share a prefix, which is what forces the tie to be broken on
	// the whole key.
	first := []byte("aaaaaaaa-one")
	second := []byte("aaaaaaaa-two")
	require.Equal(t, keyPrefix(first), keyPrefix(second))
}
