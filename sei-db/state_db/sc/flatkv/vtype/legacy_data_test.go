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
	ld := NewLegacyData().SetValue(value)

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
	require.Equal(t, value, rt.GetValue())
}

func TestLegacyNewWithValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData().SetValue(value)
	require.Equal(t, LegacyDataVersion0, ld.GetSerializationVersion())
	require.Equal(t, value, ld.GetValue())
}

func TestLegacyNewEmpty(t *testing.T) {
	ld := NewLegacyData()
	require.Equal(t, LegacyDataVersion0, ld.GetSerializationVersion())
	require.Empty(t, ld.GetValue())
}

func TestLegacySerializeLength(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData().SetValue(value)
	require.Len(t, ld.Serialize(), legacyHeaderLength+len(value))
}

func TestLegacySerializeLength_Empty(t *testing.T) {
	ld := NewLegacyData()
	require.Len(t, ld.Serialize(), legacyHeaderLength)
}

func TestLegacyRoundTrip_WithValue(t *testing.T) {
	value := bytes.Repeat([]byte{0xab}, 1000)
	ld := NewLegacyData().SetValue(value)

	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, value, rt.GetValue())
}

func TestLegacyRoundTrip_EmptyValue(t *testing.T) {
	ld := NewLegacyData()

	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.Empty(t, rt.GetValue())
}

func TestLegacyIsDelete_EmptyValue(t *testing.T) {
	ld := NewLegacyData()
	require.True(t, ld.IsDelete())
}

func TestLegacyIsDelete_EmptySlice(t *testing.T) {
	ld := NewLegacyData().SetValue([]byte{})
	require.True(t, ld.IsDelete())
}

func TestLegacyIsDelete_NonEmptyValue(t *testing.T) {
	ld := NewLegacyData().SetValue([]byte{0x01})
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

func TestLegacyDeserialize_HeaderOnly(t *testing.T) {
	ld := NewLegacyData()
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
	ld := NewLegacyData().SetValue([]byte{0x01})
	require.Equal(t, []byte{0x01}, ld.GetValue())
}

func TestLegacyConstantLayout_V0(t *testing.T) {
	require.Equal(t, 1, legacyHeaderLength)
}

func TestLegacyNewCopiesValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewLegacyData().SetValue(value)
	value[0] = 0xff
	require.Equal(t, byte(0x01), ld.GetValue()[0])
}

func TestNilLegacyData_Getters(t *testing.T) {
	var ld *LegacyData

	require.Equal(t, LegacyDataVersion0, ld.GetSerializationVersion())
	require.Empty(t, ld.GetValue())
}

func TestNilLegacyData_IsDelete(t *testing.T) {
	var ld *LegacyData
	require.True(t, ld.IsDelete())
}

func TestNilLegacyData_Serialize(t *testing.T) {
	var ld *LegacyData
	s := ld.Serialize()
	require.Len(t, s, legacyHeaderLength)
}

func TestNilLegacyData_SerializeRoundTrips(t *testing.T) {
	var ld *LegacyData
	rt, err := DeserializeLegacyData(ld.Serialize())
	require.NoError(t, err)
	require.True(t, rt.IsDelete())
	require.Empty(t, rt.GetValue())
}

func TestNilLegacyData_SettersAutoCreate(t *testing.T) {
	var l1 *LegacyData
	l1 = l1.SetValue([]byte{0xAB})
	require.NotNil(t, l1)
	require.Equal(t, []byte{0xAB}, l1.GetValue())
}

func TestLegacyData_SetValueOverwrite(t *testing.T) {
	ld := NewLegacyData().SetValue([]byte{0x01, 0x02, 0x03})
	ld = ld.SetValue([]byte{0xAA})
	require.Equal(t, []byte{0xAA}, ld.GetValue())
}

func TestLegacyData_SetValueNil(t *testing.T) {
	ld := NewLegacyData().SetValue([]byte{0x01})
	ld = ld.SetValue(nil)
	require.Empty(t, ld.GetValue())
	require.True(t, ld.IsDelete())
}
