package vtype

import (
	"bytes"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeSerializationGoldenFile_V0(t *testing.T) {
	bytecode := []byte{0x60, 0x80, 0x60, 0x40, 0x52} // PUSH1 0x80 PUSH1 0x40 MSTORE
	cd := NewCodeData().SetBytecode(bytecode).
		SetBlockHeight(100)

	serialized := cd.Serialize()

	golden := filepath.Join(testdataDir, "code_data_v0.hex")
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

	rt, err := DeserializeCodeData(wantBytes)
	require.NoError(t, err)
	require.Equal(t, int64(100), rt.GetBlockHeight())
	require.Equal(t, bytecode, rt.GetBytecode())
}

func TestCodeNewWithBytecode(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData().SetBytecode(bytecode)
	require.Equal(t, CodeDataVersion0, cd.GetSerializationVersion())
	require.Equal(t, int64(0), cd.GetBlockHeight())
	require.Equal(t, bytecode, cd.GetBytecode())
}

func TestCodeNewEmpty(t *testing.T) {
	cd := NewCodeData()
	require.Equal(t, CodeDataVersion0, cd.GetSerializationVersion())
	require.Equal(t, int64(0), cd.GetBlockHeight())
	require.Empty(t, cd.GetBytecode())
}

func TestCodeSerializeLength(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData().SetBytecode(bytecode)
	require.Len(t, cd.Serialize(), codeBytecodeStart+len(bytecode))
}

func TestCodeSerializeLength_Empty(t *testing.T) {
	cd := NewCodeData()
	require.Len(t, cd.Serialize(), codeBytecodeStart)
}

func TestCodeRoundTrip_WithBytecode(t *testing.T) {
	bytecode := bytes.Repeat([]byte{0xab}, 1000)
	cd := NewCodeData().SetBytecode(bytecode).
		SetBlockHeight(999)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, int64(999), rt.GetBlockHeight())
	require.Equal(t, bytecode, rt.GetBytecode())
}

func TestCodeRoundTrip_EmptyBytecode(t *testing.T) {
	cd := NewCodeData().
		SetBlockHeight(42)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, int64(42), rt.GetBlockHeight())
	require.Empty(t, rt.GetBytecode())
}

func TestCodeRoundTrip_MaxBlockHeight(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{0xff}).
		SetBlockHeight(math.MaxInt64)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), rt.GetBlockHeight())
	require.Equal(t, []byte{0xff}, rt.GetBytecode())
}

func TestCodeIsDelete_EmptyBytecode(t *testing.T) {
	cd := NewCodeData().SetBlockHeight(500)
	require.True(t, cd.IsDelete())
}

func TestCodeIsDelete_EmptySlice(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{})
	require.True(t, cd.IsDelete())
}

func TestCodeIsDelete_NonEmptyBytecode(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{0x01})
	require.False(t, cd.IsDelete())
}

func TestCodeDeserialize_EmptyData(t *testing.T) {
	_, err := DeserializeCodeData([]byte{})
	require.Error(t, err)
}

func TestCodeDeserialize_NilData(t *testing.T) {
	_, err := DeserializeCodeData(nil)
	require.Error(t, err)
}

func TestCodeDeserialize_TooShort(t *testing.T) {
	_, err := DeserializeCodeData([]byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestCodeDeserialize_HeaderOnly(t *testing.T) {
	cd := NewCodeData()
	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Empty(t, rt.GetBytecode())
}

func TestCodeDeserialize_UnsupportedVersion(t *testing.T) {
	data := make([]byte, codeBytecodeStart+1)
	data[0] = 0xff
	_, err := DeserializeCodeData(data)
	require.Error(t, err)
}

func TestCodeSetterChaining(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{0x01}).
		SetBlockHeight(42)

	require.Equal(t, int64(42), cd.GetBlockHeight())
	require.Equal(t, []byte{0x01}, cd.GetBytecode())
}

func TestCodeConstantLayout_V0(t *testing.T) {
	require.Equal(t, 9, codeBytecodeStart)
}

func TestCodeNewCopiesBytecode(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData().SetBytecode(bytecode)
	// Mutating the original should not affect the CodeData.
	bytecode[0] = 0xff
	require.Equal(t, byte(0x01), cd.GetBytecode()[0])
}

func TestNilCodeData_Getters(t *testing.T) {
	var cd *CodeData

	require.Equal(t, CodeDataVersion0, cd.GetSerializationVersion())
	require.Equal(t, int64(0), cd.GetBlockHeight())
	require.Empty(t, cd.GetBytecode())
}

func TestNilCodeData_IsDelete(t *testing.T) {
	var cd *CodeData
	require.True(t, cd.IsDelete())
}

func TestNilCodeData_Serialize(t *testing.T) {
	var cd *CodeData
	s := cd.Serialize()
	require.Len(t, s, codeBytecodeStart)
}

func TestNilCodeData_SerializeRoundTrips(t *testing.T) {
	var cd *CodeData
	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.True(t, rt.IsDelete())
	require.Empty(t, rt.GetBytecode())
}

func TestNilCodeData_SettersAutoCreate(t *testing.T) {
	var c1 *CodeData
	c1 = c1.SetBlockHeight(42)
	require.NotNil(t, c1)
	require.Equal(t, int64(42), c1.GetBlockHeight())

	var c2 *CodeData
	c2 = c2.SetBytecode([]byte{0xAB})
	require.NotNil(t, c2)
	require.Equal(t, []byte{0xAB}, c2.GetBytecode())
}

func TestCodeData_SetBytecodeOverwrite(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{0x01, 0x02, 0x03})
	cd.SetBytecode([]byte{0xAA})
	require.Equal(t, []byte{0xAA}, cd.GetBytecode())
}

func TestCodeData_SetBytecodeNil(t *testing.T) {
	cd := NewCodeData().SetBytecode([]byte{0x01})
	cd = cd.SetBytecode(nil)
	require.Empty(t, cd.GetBytecode())
	require.True(t, cd.IsDelete())
}
