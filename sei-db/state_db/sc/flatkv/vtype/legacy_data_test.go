package vtype

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacySerializationGoldenFile_V0(t *testing.T) {
	value := []byte{0xca, 0xfe, 0xba, 0xbe}
	ld := NewLegacyData(value).
		SetBlockHeight(100)

	serialized := ld.Serialize()

	golden := filepath.Join(testdataDir, "legacy_data_v0.hex")
	if _, err := os.Stat(golden); os.IsNotExist(err) {
		require.NoError(t, os.MkdirAll(testdataDir, 0o755))
		require.NoError(t, os.WriteFile(golden, []byte(hex.EncodeToString(serialized)), 0o644))
		t.Logf("created golden file %s — re-run to verify", golden)
		return
	}

	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	wantBytes, err := hex.DecodeString(string(want))
	require.NoError(t, err)
	require.Equal(t, wantBytes, serialized, "serialization differs from golden file")

	rt, err := DeserializeLegacyData(wantBytes)
	require.NoError(t, err)
	require.Equal(t, uint64(100), rt.GetBlockHeight())
	require.Equal(t, value, rt.GetValue())
}

func TestLegacyNewWithValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData(value)
	require.Equal(t, LegacyDataVersion0, ld.GetSerializationVersion())
	require.Equal(t, uint64(0), ld.GetBlockHeight())
	require.Equal(t, value, ld.GetValue())
}

func TestLegacyNewEmpty(t *testing.T) {
	ld := NewLegacyData(nil)
	require.Equal(t, LegacyDataVersion0, ld.GetSerializationVersion())
	require.Equal(t, uint64(0), ld.GetBlockHeight())
	require.Empty(t, ld.GetValue())
}

func TestLegacySerializeLength(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData(value)
	require.Len(t, ld.Serialize(), legacyHeaderLength+len(value))
}

func TestLegacySerializeLength_Empty(t *testing.T) {
	ld := NewLegacyData(nil)
	require.Len(t, ld.Serialize(), legacyHeaderLength)
}

func TestLegacyRoundTrip_WithValue(t *testing.T) {
	value := bytes.Repeat([]byte{0xab}, 1000)
	ld := NewLegacyData(value).
		SetBlockHeight(999)

	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(999), rt.GetBlockHeight())
	require.Equal(t, value, rt.GetValue())
}

func TestLegacyRoundTrip_EmptyValue(t *testing.T) {
	ld := NewLegacyData(nil).
		SetBlockHeight(42)

	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(42), rt.GetBlockHeight())
	require.Empty(t, rt.GetValue())
}

func TestLegacyRoundTrip_MaxBlockHeight(t *testing.T) {
	ld := NewLegacyData([]byte{0xff}).
		SetBlockHeight(0xffffffffffffffff)

	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(0xffffffffffffffff), rt.GetBlockHeight())
	require.Equal(t, []byte{0xff}, rt.GetValue())
}

func TestLegacyIsDelete_EmptyValue(t *testing.T) {
	ld := NewLegacyData(nil).SetBlockHeight(500)
	require.True(t, ld.IsDelete())
}

func TestLegacyIsDelete_EmptySlice(t *testing.T) {
	ld := NewLegacyData([]byte{})
	require.True(t, ld.IsDelete())
}

func TestLegacyIsDelete_NonEmptyValue(t *testing.T) {
	ld := NewLegacyData([]byte{0x01})
	require.False(t, ld.IsDelete())
}

func TestLegacyDeserialize_EmptyData(t *testing.T) {
	_, err := DeserializeLegacyData([]byte{})
	require.Error(t, err)
}

func TestLegacyDeserialize_NilData(t *testing.T) {
	_, err := DeserializeLegacyData(nil)
	require.Error(t, err)
}

func TestLegacyDeserialize_TooShort(t *testing.T) {
	_, err := DeserializeLegacyData([]byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestLegacyDeserialize_HeaderOnly(t *testing.T) {
	ld := NewLegacyData(nil)
	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Empty(t, rt.GetValue())
}

func TestLegacyDeserialize_UnsupportedVersion(t *testing.T) {
	data := make([]byte, legacyHeaderLength+1)
	data[0] = 0xff
	_, err := DeserializeLegacyData(data)
	require.Error(t, err)
}

func TestLegacySetterChaining(t *testing.T) {
	ld := NewLegacyData([]byte{0x01}).
		SetBlockHeight(42)

	require.Equal(t, uint64(42), ld.GetBlockHeight())
	require.Equal(t, []byte{0x01}, ld.GetValue())
}

func TestLegacyConstantLayout_V0(t *testing.T) {
	require.Equal(t, 9, legacyHeaderLength)
}

func TestLegacyNewCopiesValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData(value)
	value[0] = 0xff
	require.Equal(t, byte(0x01), ld.GetValue()[0])
}
