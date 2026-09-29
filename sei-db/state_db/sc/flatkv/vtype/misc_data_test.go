package vtype

import (
	"bytes"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiscSerializationGoldenFile_V0(t *testing.T) {
	value := []byte{0xca, 0xfe, 0xba, 0xbe}
	ld := NewMiscData().SetBlockHeight(100).SetValue(value)

	serialized := ld.Serialize()

	golden := filepath.Join(testdataDir, "misc_data_v0.hex")
	if _, err := os.Stat(golden); os.IsNotExist(err) {
		require.NoError(t, os.MkdirAll(testdataDir, 0o755))
		require.NoError(t, os.WriteFile(golden, []byte(hex.EncodeToString(serialized)), 0o644))
		t.Logf("created golden file %s — re-run to verify", golden)
		return
	}

	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	wantBytes, err := hex.DecodeString(strings.TrimSpace(string(want)))
	require.NoError(t, err)
	require.Equal(t, wantBytes, serialized, "serialization differs from golden file")

	rt, err := DeserializeMiscData(wantBytes)
	require.NoError(t, err)
	require.Equal(t, uint64(100), rt.GetBlockHeight())
	require.Equal(t, value, rt.GetValue())
}

func TestMiscNewWithValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewMiscData().SetValue(value)
	require.Equal(t, uint64(0), ld.GetBlockHeight())
	require.Equal(t, value, ld.GetValue())
}

func TestMiscNewEmpty(t *testing.T) {
	ld := NewMiscData()
	require.Equal(t, uint64(0), ld.GetBlockHeight())
	require.Empty(t, ld.GetValue())
}

func TestMiscSerializeLength(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewMiscData().SetValue(value)
	require.Len(t, ld.Serialize(), miscHeaderLength+len(value))
}

func TestMiscSerializeLength_Empty(t *testing.T) {
	ld := NewMiscData()
	require.Len(t, ld.Serialize(), miscHeaderLength)
}

func TestMiscRoundTrip_WithValue(t *testing.T) {
	value := bytes.Repeat([]byte{0xab}, 1000)
	ld := NewMiscData().SetBlockHeight(999).SetValue(value)

	rt, err := DeserializeMiscData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(999), rt.GetBlockHeight())
	require.Equal(t, value, rt.GetValue())
}

func TestMiscRoundTrip_EmptyValue(t *testing.T) {
	ld := NewMiscData().SetBlockHeight(42)

	rt, err := DeserializeMiscData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(42), rt.GetBlockHeight())
	require.Empty(t, rt.GetValue())
}

func TestMiscRoundTrip_MaxBlockHeight(t *testing.T) {
	ld := NewMiscData().SetBlockHeight(math.MaxUint64).SetValue([]byte{0xff})

	rt, err := DeserializeMiscData(ld.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64), rt.GetBlockHeight())
	require.Equal(t, []byte{0xff}, rt.GetValue())
}

func TestMiscSetValue_EmptySliceIsPresent(t *testing.T) {
	ld := NewMiscData().SetValue([]byte{})
	require.NotNil(t, ld.GetValue(), "empty value is present data, not absence")
	require.Empty(t, ld.GetValue())
}

func TestMiscDeserialize_EmptyData(t *testing.T) {
	_, err := DeserializeMiscData([]byte{})
	require.Error(t, err)
}

func TestMiscDeserialize_NilData(t *testing.T) {
	_, err := DeserializeMiscData(nil)
	require.Error(t, err)
}

func TestMiscDeserialize_TooShort(t *testing.T) {
	_, err := DeserializeMiscData([]byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestMiscDeserialize_HeaderOnly(t *testing.T) {
	ld := NewMiscData()
	rt, err := DeserializeMiscData(ld.Serialize())
	require.NoError(t, err)
	require.NotNil(t, rt.GetValue(), "an empty value on disk is present data, not absence")
	require.Empty(t, rt.GetValue())
}

func TestMiscDeserialize_UnsupportedVersion(t *testing.T) {
	data := make([]byte, miscHeaderLength+1)
	data[0] = 0xff
	_, err := DeserializeMiscData(data)
	require.Error(t, err)
}

func TestMiscSetterChaining(t *testing.T) {
	ld := NewMiscData().SetValue([]byte{0x01}).SetBlockHeight(42)
	require.Equal(t, uint64(42), ld.GetBlockHeight())
	require.Equal(t, []byte{0x01}, ld.GetValue())
}

func TestMiscConstantLayout_V0(t *testing.T) {
	require.Equal(t, 9, miscHeaderLength)
}

func TestMiscNewCopiesValue(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03}
	ld := NewMiscData().SetValue(value)
	value[0] = 0xff
	require.Equal(t, byte(0x01), ld.GetValue()[0])
}

func TestMiscData_SetValueOverwrite(t *testing.T) {
	ld := NewMiscData().SetValue([]byte{0x01, 0x02, 0x03})
	ld = ld.SetValue([]byte{0xAA})
	require.Equal(t, []byte{0xAA}, ld.GetValue())
}

func TestMiscData_SetValueNil(t *testing.T) {
	ld := NewMiscData().SetValue([]byte{0x01})
	ld = ld.SetValue(nil)
	require.NotNil(t, ld.GetValue(), "nil input is normalized to present empty data")
	require.Empty(t, ld.GetValue())
}
