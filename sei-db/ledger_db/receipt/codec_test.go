package receipt

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReceiptDataRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data receiptData
	}{
		{
			name: "typical",
			data: receiptData{BlockNumber: 1234567, TxOffset: 42, TxLength: 100, Body: []byte("receipt-body")},
		},
		{
			name: "empty body",
			data: receiptData{BlockNumber: 1, TxOffset: 0, TxLength: 0, Body: []byte{}},
		},
		{
			name: "max field values",
			data: receiptData{BlockNumber: ^uint64(0), TxOffset: ^uint32(0), TxLength: ^uint32(0), Body: []byte{0x00, 0xff}},
		},
		{
			name: "zeroed metadata (location unknown)",
			data: receiptData{BlockNumber: 0, TxOffset: 0, TxLength: 0, Body: []byte("body")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeReceiptData(tc.data)
			require.Len(t, encoded, receiptDataV1HeaderLen+len(tc.data.Body))
			require.Equal(t, receiptDataV1, encoded[0])

			got, err := decodeReceiptData(encoded)
			require.NoError(t, err)
			require.Equal(t, tc.data.BlockNumber, got.BlockNumber)
			require.Equal(t, tc.data.TxOffset, got.TxOffset)
			require.Equal(t, tc.data.TxLength, got.TxLength)
			require.Equal(t, tc.data.Body, got.Body)
		})
	}
}

// TestReceiptDataWireLayout pins the exact on-disk byte layout so an accidental
// field reorder or width change is caught rather than silently corrupting
// stored receipts.
func TestReceiptDataWireLayout(t *testing.T) {
	data := receiptData{BlockNumber: 0x0102030405060708, TxOffset: 0x0A0B0C0D, TxLength: 0x11121314, Body: []byte("xyz")}
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
	encoded := encodeReceiptData(receiptData{BlockNumber: 5, TxOffset: 1, TxLength: 2, Body: []byte("abc")})
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

	t.Run("unknown version", func(t *testing.T) {
		buf := make([]byte, receiptDataV1HeaderLen)
		buf[0] = 0xFF
		_, err := decodeReceiptData(buf)
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
