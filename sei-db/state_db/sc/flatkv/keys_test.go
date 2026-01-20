package flatkv

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlatKVPrefixEnd(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		expect []byte
	}{
		{"nil", nil, nil},
		{"empty", []byte{}, nil},
		{"simple", []byte{0x01}, []byte{0x02}},
		{"carry", []byte{0x01, 0xFF}, []byte{0x02}},
		{"multi-carry", []byte{0x01, 0xFF, 0xFF}, []byte{0x02}},
		{"all-ff", []byte{0xFF, 0xFF}, nil},
		{"mixed", []byte{0xAA, 0xFF, 0x05}, []byte{0xAA, 0xFF, 0x06}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PrefixEnd(tc.prefix)
			require.Equal(t, tc.expect, got)
		})
	}
}

func TestFlatKVAccountValueEncoding(t *testing.T) {
	// Deterministic seed so failures are reproducible.
	const seed = int64(1)
	rng := rand.New(rand.NewSource(seed))

	randomBytes := func(n int) []byte {
		b := make([]byte, n)
		rng.Read(b)
		return b
	}

	t.Run("RoundTrip", func(t *testing.T) {
		var balance Word
		copy(balance[:], randomBytes(WordLen))
		var codeHash CodeHash
		copy(codeHash[:], randomBytes(CodeHashLen))

		original := AccountValue{
			Balance:  balance,
			Nonce:    rng.Uint64(),
			CodeHash: codeHash,
		}

		encoded := EncodeAccountValue(original)
		require.Equal(t, accountValueEncodedLen, len(encoded))

		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
	})

	t.Run("RoundTripZeroValues", func(t *testing.T) {
		original := AccountValue{
			Balance:  Word{},
			Nonce:    0,
			CodeHash: CodeHash{},
		}

		encoded := EncodeAccountValue(original)
		require.Equal(t, accountValueEncodedLen, len(encoded))

		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
	})

	t.Run("InvalidLength", func(t *testing.T) {
		_, err := DecodeAccountValue([]byte{0x00})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid account value length")
	})

	t.Run("NonceIsBigEndianUint64", func(t *testing.T) {
		original := AccountValue{
			Nonce: math.MaxUint64,
		}
		encoded := EncodeAccountValue(original)
		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original.Nonce, decoded.Nonce)
	})
}

func TestFlatKVTypeConversions(t *testing.T) {
	t.Run("AddressFromBytes", func(t *testing.T) {
		valid := make([]byte, AddressLen)
		_, ok := AddressFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, AddressLen-1)
		_, ok = AddressFromBytes(invalid)
		require.False(t, ok)
	})

	t.Run("CodeHashFromBytes", func(t *testing.T) {
		valid := make([]byte, CodeHashLen)
		_, ok := CodeHashFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, CodeHashLen+1)
		_, ok = CodeHashFromBytes(invalid)
		require.False(t, ok)
	})

	t.Run("SlotFromBytes", func(t *testing.T) {
		valid := make([]byte, SlotLen)
		_, ok := SlotFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, SlotLen+1)
		_, ok = SlotFromBytes(invalid)
		require.False(t, ok)
	})
}
