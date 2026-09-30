package apphash

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// filledHash returns a hash whose every byte is b.
func filledHash(b byte) [32]byte {
	var h [32]byte
	for i := range h {
		h[i] = b
	}
	return h
}

// goldenData returns the AppHashData whose serialization and hash are pinned by the tests below.
func goldenData() *AppHashData {
	return NewAppHashData(
		0x0102030405060708,
		filledHash(0xa1),
		filledHash(0xb2),
		filledHash(0xc3),
		filledHash(0xd4),
		filledHash(0xe5),
	)
}

// goldenSerialization is the version 1 wire format of goldenData(), field by field.
var goldenSerialization = "01" +
	"0102030405060708" +
	strings.Repeat("a1", 32) +
	strings.Repeat("b2", 32) +
	strings.Repeat("c3", 32) +
	strings.Repeat("d4", 32) +
	strings.Repeat("e5", 32)

// goldenHash is SHA-256 of "sei-apphash" followed by goldenSerialization, computed independently of this package.
const goldenHash = "8016253cd17f4d1db6e1fbce6cf2159f08878acb37fc458a3c80c6de3549c0c0"

func TestConstructorAndGetters(t *testing.T) {
	ahd := goldenData()

	require.Equal(t, uint8(1), ahd.Version())
	require.Equal(t, uint64(0x0102030405060708), ahd.BlockHeight())
	require.Equal(t, filledHash(0xa1), ahd.BlockHash())
	require.Equal(t, filledHash(0xb2), ahd.StateHash())
	require.Equal(t, filledHash(0xc3), ahd.BUD())
	require.Equal(t, filledHash(0xd4), ahd.ReceiptHash())
	require.Equal(t, filledHash(0xe5), ahd.PreviousAppHash())
}

func TestSerializeLayout(t *testing.T) {
	serialized := goldenData().Serialize()

	require.Len(t, serialized, 169)
	require.Equal(t, goldenSerialization, hex.EncodeToString(serialized))
}

func TestSerializeIsDeterministic(t *testing.T) {
	require.Equal(t, goldenData().Serialize(), goldenData().Serialize())
}

func TestHashGoldenVector(t *testing.T) {
	hash := goldenData().AppHash()

	require.Equal(t, goldenHash, hex.EncodeToString(hash[:]))
}

func TestRoundTrip(t *testing.T) {
	original := goldenData()

	decoded, err := Deserialize(original.Serialize())
	require.NoError(t, err)

	require.Equal(t, original.Version(), decoded.Version())
	require.Equal(t, original.BlockHeight(), decoded.BlockHeight())
	require.Equal(t, original.BlockHash(), decoded.BlockHash())
	require.Equal(t, original.StateHash(), decoded.StateHash())
	require.Equal(t, original.BUD(), decoded.BUD())
	require.Equal(t, original.ReceiptHash(), decoded.ReceiptHash())
	require.Equal(t, original.PreviousAppHash(), decoded.PreviousAppHash())
	require.Equal(t, original.Serialize(), decoded.Serialize())
	require.Equal(t, original.AppHash(), decoded.AppHash())
}

func TestEveryFieldAffectsHash(t *testing.T) {
	base := goldenData().AppHash()
	other := filledHash(0x00)

	variants := map[string]*AppHashData{
		"blockHeight": NewAppHashData(0, filledHash(0xa1), filledHash(0xb2), filledHash(0xc3), filledHash(0xd4),
			filledHash(0xe5)),
		"blockHash": NewAppHashData(0x0102030405060708, other, filledHash(0xb2), filledHash(0xc3),
			filledHash(0xd4), filledHash(0xe5)),
		"stateHash": NewAppHashData(0x0102030405060708, filledHash(0xa1), other, filledHash(0xc3),
			filledHash(0xd4), filledHash(0xe5)),
		"bud": NewAppHashData(0x0102030405060708, filledHash(0xa1), filledHash(0xb2), other,
			filledHash(0xd4), filledHash(0xe5)),
		"receiptHash": NewAppHashData(0x0102030405060708, filledHash(0xa1), filledHash(0xb2), filledHash(0xc3),
			other, filledHash(0xe5)),
		"previousAppHash": NewAppHashData(0x0102030405060708, filledHash(0xa1), filledHash(0xb2),
			filledHash(0xc3), filledHash(0xd4), other),
	}

	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, base, variant.AppHash())
		})
	}
}

func TestDeserializeRejectsWrongLength(t *testing.T) {
	serialized := goldenData().Serialize()

	cases := map[string][]byte{
		"nil":            nil,
		"empty":          {},
		"short":          serialized[:len(serialized)-1],
		"long":           append(bytes.Clone(serialized), 0x00),
		"versioned only": {0x01},
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Deserialize(data)
			require.Error(t, err)
		})
	}
}

func TestDeserializeRejectsUnknownVersion(t *testing.T) {
	for _, version := range []byte{0x00, 0x02, 0xff} {
		data := goldenData().Serialize()
		data[0] = version

		_, err := Deserialize(data)
		require.Error(t, err, "version %d", version)
	}
}
