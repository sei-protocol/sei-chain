package gigareceipt

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestReceiptDataRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data receiptData
	}{
		{
			name: "typical",
			data: receiptData{
				Header: TxHeader{BlockNumber: 1234567, Offset: 42, Length: 100},
				Body:   []byte("receipt-body"),
			},
		},
		{
			name: "empty body",
			data: receiptData{Header: TxHeader{BlockNumber: 1, Offset: 0, Length: 0}, Body: []byte{}},
		},
		{
			name: "max field values",
			data: receiptData{
				Header: TxHeader{BlockNumber: ^uint64(0), Offset: ^uint32(0), Length: ^uint32(0)},
				Body:   []byte{0x00, 0xff},
			},
		},
		{
			name: "zeroed metadata (location unknown)",
			data: receiptData{Header: TxHeader{BlockNumber: 0, Offset: 0, Length: 0}, Body: []byte("body")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeReceiptData(tc.data)
			require.Len(t, encoded, receiptDataV1HeaderLen+len(tc.data.Body))
			require.Equal(t, receiptDataV1, encoded[0])

			got, err := decodeReceiptData(encoded)
			require.NoError(t, err)
			require.Equal(t, tc.data.Header, got.Header)
			require.Equal(t, tc.data.Body, got.Body)
		})
	}
}

// TestReceiptDataWireLayout pins the exact on-disk byte layout so an accidental
// field reorder or width change is caught rather than silently corrupting
// stored receipts.
func TestReceiptDataWireLayout(t *testing.T) {
	data := receiptData{
		Header: TxHeader{BlockNumber: 0x0102030405060708, Offset: 0x0A0B0C0D, Length: 0x11121314},
		Body:   []byte("xyz"),
	}
	encoded := encodeReceiptData(data)

	require.Equal(t, byte(1), encoded[0], "version byte")
	require.Equal(t, uint64(0x0102030405060708), binary.BigEndian.Uint64(encoded[versionLen:]))
	require.Equal(t, uint32(0x0A0B0C0D), binary.BigEndian.Uint32(encoded[versionLen+blockNumberLen:]))
	require.Equal(t, uint32(0x11121314), binary.BigEndian.Uint32(encoded[versionLen+blockNumberLen+txOffsetLen:]))
	require.Equal(t, []byte("xyz"), encoded[receiptDataV1HeaderLen:])
}

// TestDecodeReceiptDataBodyAliasesInput documents that Body is a view into the
// input buffer (no copy), matching how callers unmarshal over litt's shared
// read buffer.
func TestDecodeReceiptDataBodyAliasesInput(t *testing.T) {
	encoded := encodeReceiptData(receiptData{
		Header: TxHeader{BlockNumber: 5, Offset: 1, Length: 2},
		Body:   []byte("abc"),
	})
	got, err := decodeReceiptData(encoded)
	require.NoError(t, err)
	require.Same(t, &encoded[receiptDataV1HeaderLen], &got.Body[0])
}

func TestDecodeReceiptDataErrors(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		_, err := decodeReceiptData(nil)
		require.Error(t, err)
	})

	t.Run("too short for v1 header", func(t *testing.T) {
		short := make([]byte, receiptDataV1HeaderLen-1)
		short[0] = receiptDataV1
		_, err := decodeReceiptData(short)
		require.Error(t, err)
	})

	t.Run("header only, empty body decodes", func(t *testing.T) {
		buf := make([]byte, receiptDataV1HeaderLen)
		buf[0] = receiptDataV1
		got, err := decodeReceiptData(buf)
		require.NoError(t, err)
		require.Empty(t, got.Body)
	})
}

// TestDecodeReceiptDataRejectsReservedVersions pins that every leading byte below
// minProtobufTagByte other than a known version is rejected rather than read as legacy.
func TestDecodeReceiptDataRejectsReservedVersions(t *testing.T) {
	for version := byte(0); version < minProtobufTagByte; version++ {
		if version == receiptDataV1 {
			continue
		}
		raw := make([]byte, receiptDataV1HeaderLen+4)
		raw[0] = version
		_, err := decodeReceiptData(raw)
		require.ErrorContains(t, err, "unsupported receipt encoding version", "leading byte 0x%02x", version)
	}
}

// TestDecodeReceiptDataLegacyFallback documents that a value whose leading byte is
// minProtobufTagByte or above is treated as a legacy, pre-prefix receipt (a bare
// marshaled Receipt with no metadata header), so values written before this codec
// existed keep decoding after an upgrade.
func TestDecodeReceiptDataLegacyFallback(t *testing.T) {
	t.Run("marshaled receipt", func(t *testing.T) {
		raw, err := (&types.Receipt{TxHashHex: "0xabc", BlockNumber: 7, Status: 1}).Marshal()
		require.NoError(t, err)
		require.GreaterOrEqual(t, raw[0], minProtobufTagByte)

		got, err := decodeReceiptData(raw)
		require.NoError(t, err)
		require.Equal(t, TxHeader{}, got.Header)
		var r types.Receipt
		require.NoError(t, r.Unmarshal(got.Body))
		require.Equal(t, uint64(7), r.BlockNumber)
	})

	t.Run("leading byte is a real protobuf tag", func(t *testing.T) {
		// 0x08 is the wire-format tag for field 1 (varint) — the smallest tag byte a real
		// marshaled Receipt can start with, and the case closest to colliding with receiptDataV1.
		raw := []byte{0x08, 0x01, 0x99}
		got, err := decodeReceiptData(raw)
		require.NoError(t, err)
		require.Equal(t, TxHeader{}, got.Header)
		require.Equal(t, raw, got.Body)
	})

	t.Run("arbitrary unknown leading byte", func(t *testing.T) {
		raw := []byte{0xFF, 0x00, 0x01}
		got, err := decodeReceiptData(raw)
		require.NoError(t, err)
		require.Equal(t, TxHeader{}, got.Header)
		require.Equal(t, raw, got.Body)
	})
}
