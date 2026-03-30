package vtype

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStorageSerializationGoldenFile_V0(t *testing.T) {
	val := toArray32(leftPad32([]byte{0xde, 0xad}))
	sd := NewStorageData().
		SetBlockHeight(100).
		SetValue(val)

	serialized := sd.Serialize()

	golden := filepath.Join(testdataDir, "storage_data_v0.hex")
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

	rt, err := DeserializeStorageData(wantBytes)
	require.NoError(t, err)
	require.Equal(t, uint64(100), rt.GetBlockHeight())
	require.Equal(t, val, rt.GetValue())
}

func TestStorageNewZeroInitialized(t *testing.T) {
	sd := NewStorageData()
	var zero [32]byte
	require.Equal(t, StorageDataVersion0, sd.GetSerializationVersion())
	require.Equal(t, uint64(0), sd.GetBlockHeight())
	require.Equal(t, &zero, sd.GetValue())
}

func TestStorageSerializeLength(t *testing.T) {
	sd := NewStorageData()
	require.Len(t, sd.Serialize(), storageDataLength)
}

func TestStorageRoundTrip_AllFieldsSet(t *testing.T) {
	val := toArray32(leftPad32([]byte{0xff, 0xee}))
	sd := NewStorageData().
		SetBlockHeight(999).
		SetValue(val)

	rt, err := DeserializeStorageData(sd.Serialize())
	require.NoError(t, err)
	require.Equal(t, uint64(999), rt.GetBlockHeight())
	require.Equal(t, val, rt.GetValue())
}

func TestStorageRoundTrip_ZeroValues(t *testing.T) {
	sd := NewStorageData()
	rt, err := DeserializeStorageData(sd.Serialize())
	require.NoError(t, err)
	var zero [32]byte
	require.Equal(t, uint64(0), rt.GetBlockHeight())
	require.Equal(t, &zero, rt.GetValue())
}

func TestStorageRoundTrip_MaxValues(t *testing.T) {
	maxVal := toArray32(bytes.Repeat([]byte{0xff}, 32))
	maxBlockHeight := uint64(0xffffffffffffffff)

	sd := NewStorageData().
		SetBlockHeight(maxBlockHeight).
		SetValue(maxVal)

	rt, err := DeserializeStorageData(sd.Serialize())
	require.NoError(t, err)
	require.Equal(t, maxBlockHeight, rt.GetBlockHeight())
	require.Equal(t, maxVal, rt.GetValue())
}

func TestStorageIsDelete_ZeroValue(t *testing.T) {
	sd := NewStorageData().SetBlockHeight(500)
	require.True(t, sd.IsDelete())
}

func TestStorageIsDelete_NonZeroValue(t *testing.T) {
	sd := NewStorageData().SetValue(toArray32(leftPad32([]byte{1})))
	require.False(t, sd.IsDelete())
}

func TestStorageDeserialize_EmptyData(t *testing.T) {
	_, err := DeserializeStorageData([]byte{})
	require.Error(t, err)
}

func TestStorageDeserialize_NilData(t *testing.T) {
	_, err := DeserializeStorageData(nil)
	require.Error(t, err)
}

func TestStorageDeserialize_TooShort(t *testing.T) {
	_, err := DeserializeStorageData([]byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestStorageDeserialize_TooLong(t *testing.T) {
	_, err := DeserializeStorageData(make([]byte, storageDataLength+1))
	require.Error(t, err)
}

func TestStorageDeserialize_UnsupportedVersion(t *testing.T) {
	data := make([]byte, storageDataLength)
	data[0] = 0xff
	_, err := DeserializeStorageData(data)
	require.Error(t, err)
}

func TestStorageSetterChaining(t *testing.T) {
	sd := NewStorageData().
		SetBlockHeight(1).
		SetValue(toArray32(leftPad32([]byte{2})))

	require.Equal(t, uint64(1), sd.GetBlockHeight())
}

func TestStorageConstantLayout_V0(t *testing.T) {
	require.Equal(t, 41, storageDataLength)
}
