package types

import (
	"bytes"
	"testing"

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
