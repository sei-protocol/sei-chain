package vtype

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeSerializationGoldenFile_V0(t *testing.T) {
	bytecode := []byte{0x60, 0x80, 0x60, 0x40, 0x52} // PUSH1 0x80 PUSH1 0x40 MSTORE
	cd := NewCodeData(bytecode).
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
	require.Equal(t, uint64(100), rt.GetBlockHeight())
	require.Equal(t, bytecode, rt.GetBytecode())
}

func TestCodeNewWithBytecode(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData(bytecode)
	require.Equal(t, CodeDataVersion0, cd.GetSerializationVersion())
	require.Equal(t, uint64(0), cd.GetBlockHeight())
	require.Equal(t, bytecode, cd.GetBytecode())
}

func TestCodeNewEmpty(t *testing.T) {
	cd := NewCodeData(nil)
	require.Equal(t, CodeDataVersion0, cd.GetSerializationVersion())
	require.Equal(t, uint64(0), cd.GetBlockHeight())
	require.Empty(t, cd.GetBytecode())
}

func TestCodeSerializeLength(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData(bytecode)
	require.Len(t, cd.Serialize(), codeBytecodeStart+len(bytecode))
}

func TestCodeSerializeLength_Empty(t *testing.T) {
	cd := NewCodeData(nil)
	require.Len(t, cd.Serialize(), codeBytecodeStart)
}

func TestCodeRoundTrip_WithBytecode(t *testing.T) {
	bytecode := bytes.Repeat([]byte{0xab}, 1000)
	cd := NewCodeData(bytecode).
		SetBlockHeight(999)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(999), rt.GetBlockHeight())
	require.Equal(t, bytecode, rt.GetBytecode())
}

func TestCodeRoundTrip_EmptyBytecode(t *testing.T) {
	cd := NewCodeData(nil).
		SetBlockHeight(42)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(42), rt.GetBlockHeight())
	require.Empty(t, rt.GetBytecode())
}

func TestCodeRoundTrip_MaxBlockHeight(t *testing.T) {
	cd := NewCodeData([]byte{0xff}).
		SetBlockHeight(0xffffffffffffffff)

	rt, err := DeserializeCodeData(cd.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(0xffffffffffffffff), rt.GetBlockHeight())
	require.Equal(t, []byte{0xff}, rt.GetBytecode())
}

func TestCodeIsDelete_EmptyBytecode(t *testing.T) {
	cd := NewCodeData(nil).SetBlockHeight(500)
	require.True(t, cd.IsDelete())
}

func TestCodeIsDelete_EmptySlice(t *testing.T) {
	cd := NewCodeData([]byte{})
	require.True(t, cd.IsDelete())
}

func TestCodeIsDelete_NonEmptyBytecode(t *testing.T) {
	cd := NewCodeData([]byte{0x01})
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
	cd := NewCodeData(nil)
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
	cd := NewCodeData([]byte{0x01}).
		SetBlockHeight(42)

	require.Equal(t, uint64(42), cd.GetBlockHeight())
	require.Equal(t, []byte{0x01}, cd.GetBytecode())
}

func TestCodeConstantLayout_V0(t *testing.T) {
	require.Equal(t, 9, codeBytecodeStart)
}

func TestCodeNewCopiesBytecode(t *testing.T) {
	bytecode := []byte{0x01, 0x02, 0x03}
	cd := NewCodeData(bytecode)
	// Mutating the original should not affect the CodeData.
	bytecode[0] = 0xff
	require.Equal(t, byte(0x01), cd.GetBytecode()[0])
}
