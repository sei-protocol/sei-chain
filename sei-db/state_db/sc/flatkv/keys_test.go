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

	t.Run("RoundTripContract", func(t *testing.T) {
		var balance Balance
		copy(balance[:], randomBytes(BalanceLen))
		var codeHash CodeHash
		copy(codeHash[:], randomBytes(CodeHashLen))

		original := AccountValue{
			Balance:  balance,
			Nonce:    rng.Uint64(),
			CodeHash: codeHash,
		}

		require.True(t, original.HasCode(), "contract should have code")

		encoded := EncodeAccountValue(original)
		require.Equal(t, accountValueContractLen, len(encoded), "contract should be 72 bytes")

		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
	})

	t.Run("RoundTripEOA", func(t *testing.T) {
		var balance Balance
		copy(balance[:], randomBytes(BalanceLen))

		original := AccountValue{
			Balance:  balance,
			Nonce:    rng.Uint64(),
			CodeHash: CodeHash{}, // EOA has no code
		}

		require.False(t, original.HasCode(), "EOA should not have code")

		encoded := EncodeAccountValue(original)
		require.Equal(t, accountValueEOALen, len(encoded), "EOA should be 40 bytes")

		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
	})

	t.Run("RoundTripZeroEOA", func(t *testing.T) {
		// Completely empty account (zero balance, zero nonce, no code)
		original := AccountValue{
			Balance:  Balance{},
			Nonce:    0,
			CodeHash: CodeHash{},
		}

		require.False(t, original.HasCode())

		encoded := EncodeAccountValue(original)
		require.Equal(t, accountValueEOALen, len(encoded), "zero EOA should be 40 bytes")

		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original, decoded)
	})

	t.Run("InvalidLength", func(t *testing.T) {
		// Too short
		_, err := DecodeAccountValue([]byte{0x00})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid account value length")

		// In between EOA and Contract lengths
		_, err = DecodeAccountValue(make([]byte, 50))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid account value length")

		// Too long
		_, err = DecodeAccountValue(make([]byte, 100))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid account value length")
	})

	t.Run("NonceIsBigEndianUint64", func(t *testing.T) {
		// Test with EOA
		original := AccountValue{
			Nonce: math.MaxUint64,
		}
		encoded := EncodeAccountValue(original)
		decoded, err := DecodeAccountValue(encoded)
		require.NoError(t, err)
		require.Equal(t, original.Nonce, decoded.Nonce)

		// Test with Contract
		var codeHash CodeHash
		copy(codeHash[:], randomBytes(CodeHashLen))
		originalContract := AccountValue{
			Nonce:    math.MaxUint64,
			CodeHash: codeHash,
		}
		encodedContract := EncodeAccountValue(originalContract)
		decodedContract, err := DecodeAccountValue(encodedContract)
		require.NoError(t, err)
		require.Equal(t, originalContract.Nonce, decodedContract.Nonce)
	})

	t.Run("HasCodeMethod", func(t *testing.T) {
		// EOA - no code
		eoa := AccountValue{CodeHash: CodeHash{}}
		require.False(t, eoa.HasCode())

		// Contract - has code (any non-zero hash)
		var codeHash CodeHash
		codeHash[0] = 0x01 // Just one non-zero byte is enough
		contract := AccountValue{CodeHash: codeHash}
		require.True(t, contract.HasCode())
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

	t.Run("SlotFromBytes", func(t *testing.T) {
		valid := make([]byte, SlotLen)
		_, ok := SlotFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, SlotLen+1)
		_, ok = SlotFromBytes(invalid)
		require.False(t, ok)
	})
}

func TestLocalMetaSerialization(t *testing.T) {
	t.Run("RoundTripZero", func(t *testing.T) {
		original := &LocalMeta{CommittedVersion: 0}
		encoded := MarshalLocalMeta(original)
		require.Equal(t, localMetaSize, len(encoded))

		decoded, err := UnmarshalLocalMeta(encoded)
		require.NoError(t, err)
		require.Equal(t, original.CommittedVersion, decoded.CommittedVersion)
	})

	t.Run("RoundTripPositive", func(t *testing.T) {
		original := &LocalMeta{CommittedVersion: 12345}
		encoded := MarshalLocalMeta(original)
		require.Equal(t, localMetaSize, len(encoded))

		decoded, err := UnmarshalLocalMeta(encoded)
		require.NoError(t, err)
		require.Equal(t, original.CommittedVersion, decoded.CommittedVersion)
	})

	t.Run("RoundTripMaxInt64", func(t *testing.T) {
		original := &LocalMeta{CommittedVersion: math.MaxInt64}
		encoded := MarshalLocalMeta(original)
		require.Equal(t, localMetaSize, len(encoded))

		decoded, err := UnmarshalLocalMeta(encoded)
		require.NoError(t, err)
		require.Equal(t, original.CommittedVersion, decoded.CommittedVersion)
	})

	t.Run("InvalidLength", func(t *testing.T) {
		// Too short
		_, err := UnmarshalLocalMeta([]byte{0x00})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid LocalMeta size")

		// Too long
		_, err = UnmarshalLocalMeta(make([]byte, localMetaSize+1))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid LocalMeta size")
	})

	t.Run("BigEndianEncoding", func(t *testing.T) {
		// Verify big-endian encoding: version 0x0102030405060708
		meta := &LocalMeta{CommittedVersion: 0x0102030405060708}
		encoded := MarshalLocalMeta(meta)

		// Big-endian: most significant byte first
		require.Equal(t, byte(0x01), encoded[0])
		require.Equal(t, byte(0x02), encoded[1])
		require.Equal(t, byte(0x08), encoded[7])
	})
}
