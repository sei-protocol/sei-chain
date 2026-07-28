package types

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/klauspost/compress/s2"
	"github.com/stretchr/testify/require"
)

func TestCompressionRoundTrip(t *testing.T) {
	t.Parallel()

	// A compressible payload so the S2 case actually shrinks.
	payload := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 100)

	cases := []struct {
		name string
		algo CompressionAlgorithm
	}{
		{"none", CompressionNone},
		{"s2", CompressionS2},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			compressed, err := Compress(tc.algo, payload)
			require.NoError(t, err)

			decompressed, err := Decompress(tc.algo, compressed)
			require.NoError(t, err)
			require.Equal(t, payload, decompressed)

			if tc.algo == CompressionS2 {
				require.Less(t, len(compressed), len(payload), "s2 should shrink a repetitive payload")
			}
		})
	}
}

func TestCompressionRoundTripEmptyValue(t *testing.T) {
	t.Parallel()

	for _, algo := range []CompressionAlgorithm{CompressionNone, CompressionS2} {
		compressed, err := Compress(algo, []byte{})
		require.NoError(t, err)

		decompressed, err := Decompress(algo, compressed)
		require.NoError(t, err)
		require.Empty(t, decompressed)
	}
}

func TestCompressionAlgorithmValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, CompressionNone.Validate())
	require.NoError(t, CompressionS2.Validate())
	require.Error(t, CompressionAlgorithm(99).Validate())
}

func TestCompressUnknownAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := Compress(CompressionAlgorithm(99), []byte("data"))
	require.Error(t, err)

	_, err = Decompress(CompressionAlgorithm(99), []byte("data"))
	require.Error(t, err)
}

// TestEncodeValueCompressibleStoresCompressed verifies a shrinkable payload is stored under the S2 tag
// and round-trips.
func TestEncodeValueCompressibleStoresCompressed(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 100)

	blob, err := EncodeValue(CompressionS2, payload)
	require.NoError(t, err)
	require.Equal(t, byte(CompressionS2), blob[0], "compressible payload should be tagged S2")
	require.Less(t, len(blob), len(payload), "S2 blob (incl. tag) should be smaller than the raw payload")

	decoded, err := DecodeValue(blob)
	require.NoError(t, err)
	require.Equal(t, payload, decoded)
}

// TestEncodeValueIncompressibleStoresRaw verifies that when compression does not shrink the value it is
// stored raw (tagged None) rather than in its larger compressed form, and still round-trips.
func TestEncodeValueIncompressibleStoresRaw(t *testing.T) {
	t.Parallel()

	// Deterministic pseudo-random bytes: S2 cannot shrink these, so store-smaller must keep them raw.
	payload := make([]byte, 4096)
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // test fixture, not security-sensitive
	_, _ = rng.Read(payload)

	blob, err := EncodeValue(CompressionS2, payload)
	require.NoError(t, err)
	require.Equal(t, byte(CompressionNone), blob[0], "incompressible payload should be tagged None")
	require.Equal(t, len(payload)+1, len(blob), "raw blob should be the payload plus a one-byte tag")
	require.Equal(t, payload, blob[1:], "raw body should be the payload verbatim")

	decoded, err := DecodeValue(blob)
	require.NoError(t, err)
	require.Equal(t, payload, decoded)
}

// TestEncodeValueEmpty verifies an empty value encodes to a lone tag byte and decodes back to empty.
func TestEncodeValueEmpty(t *testing.T) {
	t.Parallel()

	blob, err := EncodeValue(CompressionS2, []byte{})
	require.NoError(t, err)
	require.Len(t, blob, 1, "an empty value encodes to just the algorithm tag")
	require.Equal(t, byte(CompressionNone), blob[0])

	decoded, err := DecodeValue(blob)
	require.NoError(t, err)
	require.Empty(t, decoded)
}

// TestDecodeValueEmptyInput verifies DecodeValue rejects a blob missing the tag byte.
func TestDecodeValueEmptyInput(t *testing.T) {
	t.Parallel()

	_, err := DecodeValue(nil)
	require.Error(t, err)

	_, err = DecodeValue([]byte{})
	require.Error(t, err)
}

// TestDecodeValueUnknownTag verifies DecodeValue surfaces an unknown per-value algorithm tag.
func TestDecodeValueUnknownTag(t *testing.T) {
	t.Parallel()

	_, err := DecodeValue([]byte{99, 'x'})
	require.Error(t, err)
}

// TestS2MaxCompressibleSizeIsSafe pins s2WorstCaseExpansion to the library's real behavior: the largest
// input EncodeValue will hand to s2.Encode must have a defined (non-negative) MaxEncodedLen, so Encode
// can never panic with ErrTooLarge. Guards against the constant drifting too small.
func TestS2MaxCompressibleSizeIsSafe(t *testing.T) {
	t.Parallel()

	maxSize := CompressionS2.MaxCompressibleSize()
	require.Equal(t, uint64(math.MaxUint32-s2WorstCaseExpansion), maxSize)
	require.LessOrEqual(t, maxSize, uint64(math.MaxInt), "cap must be representable as an int length")
	require.GreaterOrEqual(t, s2.MaxEncodedLen(int(maxSize)), 0,
		"s2.Encode would panic for the largest value EncodeValue compresses")

	require.Equal(t, uint64(math.MaxUint64), CompressionNone.MaxCompressibleSize(),
		"CompressionNone imposes no size limit of its own")
}
