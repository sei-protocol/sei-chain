package vtype

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// roundTripCase is one canonical encoding that must survive a decode and re-encode unchanged.
type roundTripCase struct {
	// The name of the subtest.
	name string

	// The canonical encoding.
	data []byte
}

// Every canonical account encoding re-encodes to the same bytes, through both AppendAccountData() and Serialize().
func TestAccountData_EncodingRoundTrip(t *testing.T) {
	v0 := []byte{byte(AccountDataVersion0)}
	cases := []roundTripCase{
		{"compact zero", make([]byte, accountCompactLength)},
		{"compact zero code hash", concat(v0, be64(7), leftPad32([]byte{0x01, 0x02}), be64(3))},
		{"full non-zero code hash",
			concat(v0, be64(9), leftPad32([]byte{0xff}), be64(5), bytes.Repeat([]byte{0xcd}, 32))},
		{"full code hash last byte set",
			concat(v0, be64(1), make([]byte, BalanceLength), be64(0), leftPad32([]byte{1}))},
		{"golden compact", readGolden(t, "account_data_v0_compact.hex")},
		{"golden full", readGolden(t, "account_data_v0_full.hex")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DeserializeAccountData(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.data, AppendAccountData(nil, decoded))
			require.Equal(t, tc.data, decoded.Serialize())
			require.Equal(t, concat([]byte{0xee}, tc.data), AppendAccountData([]byte{0xee}, decoded))
		})
	}
}

// Every canonical storage encoding re-encodes to the same bytes, through both AppendStorageData() and Serialize().
func TestStorageData_EncodingRoundTrip(t *testing.T) {
	v0 := []byte{byte(StorageDataVersion0)}
	cases := []roundTripCase{
		{"zero", make([]byte, storageDataLength)},
		{"non-zero", concat(v0, be64(100), leftPad32([]byte{0xde, 0xad}))},
		{"max", concat(v0, bytes.Repeat([]byte{0xff}, BlockHeightLength+StorageValueLength))},
		{"golden", readGolden(t, "storage_data_v0.hex")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DeserializeStorageData(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.data, AppendStorageData(nil, decoded))
			require.Equal(t, tc.data, decoded.Serialize())
			require.Equal(t, concat([]byte{0xee}, tc.data), AppendStorageData([]byte{0xee}, decoded))
		})
	}
}

// Every canonical code encoding re-encodes to the same bytes, through both AppendCodeData() and Serialize().
func TestCodeData_EncodingRoundTrip(t *testing.T) {
	v0 := []byte{byte(CodeDataVersion0)}
	cases := []roundTripCase{
		{"empty bytecode", concat(v0, be64(42))},
		{"non-empty bytecode", concat(v0, be64(1), []byte{0x60, 0x80, 0x60, 0x40, 0x52})},
		{"large bytecode", concat(v0, be64(999), bytes.Repeat([]byte{0xab}, 1000))},
		{"golden", readGolden(t, "code_data_v0.hex")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DeserializeCodeData(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.data, AppendCodeData(nil, decoded))
			require.Equal(t, tc.data, decoded.Serialize())
			require.Equal(t, concat([]byte{0xee}, tc.data), AppendCodeData([]byte{0xee}, decoded))
		})
	}
}

// Every canonical misc encoding re-encodes to the same bytes, through both AppendMiscData() and Serialize().
func TestMiscData_EncodingRoundTrip(t *testing.T) {
	v0 := []byte{byte(MiscDataVersion0)}
	cases := []roundTripCase{
		{"empty value", concat(v0, be64(42))},
		{"non-empty value", concat(v0, be64(1), []byte{0xca, 0xfe})},
		{"large value", concat(v0, be64(999), bytes.Repeat([]byte{0xab}, 1000))},
		{"golden", readGolden(t, "misc_data_v0.hex")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DeserializeMiscData(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.data, AppendMiscData(nil, decoded))
			require.Equal(t, tc.data, decoded.Serialize())
			require.Equal(t, concat([]byte{0xee}, tc.data), AppendMiscData([]byte{0xee}, decoded))
		})
	}
}

// readGolden returns the bytes held by a golden hex file.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(testdataDir, name))
	require.NoError(t, err)
	data, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	require.NoError(t, err)
	return data
}

// concat returns the concatenation of parts in a new slice.
func concat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// be64 returns the 8-byte big-endian encoding of v.
func be64(v uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, v)
}
