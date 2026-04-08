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

const testdataDir = "testdata"

// goldenCheck compares serialized against a golden hex file, creating it on first run.
func goldenCheck(t *testing.T, name string, serialized []byte) {
	t.Helper()
	golden := filepath.Join(testdataDir, name)
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
	require.Equal(t, wantBytes, serialized, "serialization differs from golden file %s", name)
}

// Full form: with non-zero codehash (81 bytes).
func TestSerializationGoldenFile_V0_Full(t *testing.T) {
	ad := NewAccountData().
		SetBlockHeight(100).
		SetBalance(toBalance(leftPad32([]byte{1}))).
		SetNonce(42).
		SetCodeHash(toCodeHash(bytes.Repeat([]byte{0xaa}, 32)))

	serialized := ad.Serialize()
	require.Len(t, serialized, accountDataLength)
	goldenCheck(t, "account_data_v0_full.hex", serialized)

	rt, err := DeserializeAccountData(serialized)
	require.NoError(t, err)
	require.Equal(t, int64(100), rt.GetBlockHeight())
	require.Equal(t, uint64(42), rt.GetNonce())
	require.Equal(t, toBalance(leftPad32([]byte{1})), rt.GetBalance())
	require.Equal(t, toCodeHash(bytes.Repeat([]byte{0xaa}, 32)), rt.GetCodeHash())
}

// Compact form: zero codehash omitted (49 bytes).
func TestSerializationGoldenFile_V0_Compact(t *testing.T) {
	ad := NewAccountData().
		SetBlockHeight(100).
		SetBalance(toBalance(leftPad32([]byte{1}))).
		SetNonce(42)

	serialized := ad.Serialize()
	require.Len(t, serialized, accountCompactLength)
	goldenCheck(t, "account_data_v0_compact.hex", serialized)

	rt, err := DeserializeAccountData(serialized)
	require.NoError(t, err)
	require.Equal(t, int64(100), rt.GetBlockHeight())
	require.Equal(t, uint64(42), rt.GetNonce())
	require.Equal(t, toBalance(leftPad32([]byte{1})), rt.GetBalance())
	var zeroHash CodeHash
	require.Equal(t, &zeroHash, rt.GetCodeHash())
}

func TestNewAccountData_ZeroInitialized(t *testing.T) {
	ad := NewAccountData()
	var zero [32]byte
	require.Equal(t, AccountDataVersion0, ad.GetSerializationVersion())
	require.Equal(t, int64(0), ad.GetBlockHeight())
	require.Equal(t, uint64(0), ad.GetNonce())
	require.Equal(t, (*Balance)(&zero), ad.GetBalance())
	require.Equal(t, (*CodeHash)(&zero), ad.GetCodeHash())
}

func TestSerializeLength_Compact(t *testing.T) {
	ad := NewAccountData()
	require.Len(t, ad.Serialize(), accountCompactLength)
}

func TestSerializeLength_Full(t *testing.T) {
	ad := NewAccountData().SetCodeHash(toCodeHash(bytes.Repeat([]byte{0x01}, 32)))
	require.Len(t, ad.Serialize(), accountDataLength)
}

func TestRoundTrip_AllFieldsSet(t *testing.T) {
	balance := toBalance(leftPad32([]byte{0xff, 0xee, 0xdd}))
	codeHash := toCodeHash(bytes.Repeat([]byte{0xab}, 32))

	ad := NewAccountData().
		SetBlockHeight(999).
		SetBalance(balance).
		SetNonce(12345).
		SetCodeHash(codeHash)

	rt, err := DeserializeAccountData(ad.Serialize())
	require.NoError(t, err)
	require.Equal(t, int64(999), rt.GetBlockHeight())
	require.Equal(t, uint64(12345), rt.GetNonce())
	require.Equal(t, balance, rt.GetBalance())
	require.Equal(t, codeHash, rt.GetCodeHash())
}

func TestRoundTrip_ZeroValues(t *testing.T) {
	ad := NewAccountData()
	serialized := ad.Serialize()
	require.Len(t, serialized, accountCompactLength, "zero codehash should produce compact form")
	rt, err := DeserializeAccountData(serialized)
	require.NoError(t, err)
	var zero [32]byte
	require.Equal(t, int64(0), rt.GetBlockHeight())
	require.Equal(t, uint64(0), rt.GetNonce())
	require.Equal(t, (*Balance)(&zero), rt.GetBalance())
	require.Equal(t, (*CodeHash)(&zero), rt.GetCodeHash())
}

func TestRoundTrip_CompactWithNonZeroFields(t *testing.T) {
	ad := NewAccountData().
		SetBlockHeight(500).
		SetBalance(toBalance(leftPad32([]byte{0x42}))).
		SetNonce(77)

	serialized := ad.Serialize()
	require.Len(t, serialized, accountCompactLength)

	rt, err := DeserializeAccountData(serialized)
	require.NoError(t, err)
	require.Equal(t, int64(500), rt.GetBlockHeight())
	require.Equal(t, uint64(77), rt.GetNonce())
	require.Equal(t, toBalance(leftPad32([]byte{0x42})), rt.GetBalance())
	var zeroHash CodeHash
	require.Equal(t, &zeroHash, rt.GetCodeHash())
}

func TestRoundTrip_MaxValues(t *testing.T) {
	maxBalance := toBalance(bytes.Repeat([]byte{0xff}, 32))
	maxCodeHash := toCodeHash(bytes.Repeat([]byte{0xff}, 32))
	maxNonce := uint64(0xffffffffffffffff)
	maxBlockHeight := int64(math.MaxInt64)

	ad := NewAccountData().
		SetBlockHeight(maxBlockHeight).
		SetBalance(maxBalance).
		SetNonce(maxNonce).
		SetCodeHash(maxCodeHash)

	rt, err := DeserializeAccountData(ad.Serialize())
	require.NoError(t, err)
	require.Equal(t, maxBlockHeight, rt.GetBlockHeight())
	require.Equal(t, maxNonce, rt.GetNonce())
	require.Equal(t, maxBalance, rt.GetBalance())
	require.Equal(t, maxCodeHash, rt.GetCodeHash())
}

func TestIsDelete_AllZeroPayload(t *testing.T) {
	ad := NewAccountData().SetBlockHeight(500)
	require.True(t, ad.IsDelete())
}

func TestIsDelete_NonZeroBalance(t *testing.T) {
	ad := NewAccountData().SetBalance(toBalance(leftPad32([]byte{1})))
	require.False(t, ad.IsDelete())
}

func TestIsDelete_NonZeroNonce(t *testing.T) {
	ad := NewAccountData().SetNonce(1)
	require.False(t, ad.IsDelete())
}

func TestIsDelete_NonZeroCodeHash(t *testing.T) {
	ad := NewAccountData().SetCodeHash(toCodeHash(bytes.Repeat([]byte{0x01}, 32)))
	require.False(t, ad.IsDelete())
}

func TestDeserialize_EmptyData(t *testing.T) {
	_, err := DeserializeAccountData([]byte{})
	require.Error(t, err)
}

func TestDeserialize_NilData(t *testing.T) {
	_, err := DeserializeAccountData(nil)
	require.Error(t, err)
}

func TestDeserialize_TooShort(t *testing.T) {
	_, err := DeserializeAccountData([]byte{0x00, 0x01, 0x02})
	require.Error(t, err)
}

func TestDeserialize_TooLong(t *testing.T) {
	_, err := DeserializeAccountData(make([]byte, accountDataLength+1))
	require.Error(t, err)
}

func TestDeserialize_BetweenCompactAndFull(t *testing.T) {
	_, err := DeserializeAccountData(make([]byte, accountCompactLength+1))
	require.Error(t, err)
}

func TestDeserialize_UnsupportedVersion(t *testing.T) {
	full := make([]byte, accountDataLength)
	full[0] = 0xff
	_, err := DeserializeAccountData(full)
	require.Error(t, err)

	compact := make([]byte, accountCompactLength)
	compact[0] = 0xff
	_, err = DeserializeAccountData(compact)
	require.Error(t, err)
}

func TestSetterChaining(t *testing.T) {
	ad := NewAccountData().
		SetBlockHeight(1).
		SetBalance(toBalance(leftPad32([]byte{2}))).
		SetNonce(3).
		SetCodeHash(toCodeHash(leftPad32([]byte{4})))

	require.Equal(t, int64(1), ad.GetBlockHeight())
	require.Equal(t, uint64(3), ad.GetNonce())
}

func TestConstantLayout_V0(t *testing.T) {
	require.Equal(t, 81, accountDataLength)
}

func TestNilAccountData_Getters(t *testing.T) {
	var ad *AccountData
	var zero [32]byte

	require.Equal(t, AccountDataVersion0, ad.GetSerializationVersion())
	require.Equal(t, int64(0), ad.GetBlockHeight())
	require.Equal(t, uint64(0), ad.GetNonce())
	require.Equal(t, (*Balance)(&zero), ad.GetBalance())
	require.Equal(t, (*CodeHash)(&zero), ad.GetCodeHash())
}

func TestNilAccountData_IsDelete(t *testing.T) {
	var ad *AccountData
	require.True(t, ad.IsDelete())
}

func TestNilAccountData_Serialize(t *testing.T) {
	var ad *AccountData
	s := ad.Serialize()
	require.Len(t, s, accountCompactLength)
	for _, b := range s {
		require.Equal(t, byte(0), b)
	}
}

func TestNilAccountData_SerializeRoundTrips(t *testing.T) {
	var ad *AccountData
	rt, err := DeserializeAccountData(ad.Serialize())
	require.NoError(t, err)
	require.True(t, rt.IsDelete())
}

func TestNilAccountData_Copy(t *testing.T) {
	var ad *AccountData
	cp := ad.Copy()
	require.NotNil(t, cp)
	require.True(t, cp.IsDelete())
	require.Len(t, cp.Serialize(), accountCompactLength)
}

func TestNilAccountData_SettersAutoCreate(t *testing.T) {
	var a1 *AccountData
	a1 = a1.SetBlockHeight(42)
	require.NotNil(t, a1)
	require.Equal(t, int64(42), a1.GetBlockHeight())

	var a2 *AccountData
	a2 = a2.SetNonce(7)
	require.NotNil(t, a2)
	require.Equal(t, uint64(7), a2.GetNonce())

	var a3 *AccountData
	bal := Balance{0x01}
	a3 = a3.SetBalance(&bal)
	require.NotNil(t, a3)
	require.Equal(t, &bal, a3.GetBalance())

	var a4 *AccountData
	ch := CodeHash{0x02}
	a4 = a4.SetCodeHash(&ch)
	require.NotNil(t, a4)
	require.Equal(t, &ch, a4.GetCodeHash())
}

func TestAccountData_CopyIndependence(t *testing.T) {
	ad := NewAccountData().SetNonce(10).SetBlockHeight(5)
	cp := ad.Copy()

	cp.SetNonce(99)
	require.Equal(t, uint64(10), ad.GetNonce(), "original must not change")
	require.Equal(t, uint64(99), cp.GetNonce())
}

func TestAccountData_SetBalanceNilZeros(t *testing.T) {
	ad := NewAccountData().
		SetBalance(toBalance(leftPad32([]byte{0xff}))).
		SetBalance(nil)
	var zero Balance
	require.Equal(t, &zero, ad.GetBalance())
}

func TestAccountData_SetCodeHashNilZeros(t *testing.T) {
	ad := NewAccountData().
		SetCodeHash(toCodeHash(bytes.Repeat([]byte{0xaa}, 32))).
		SetCodeHash(nil)
	var zero CodeHash
	require.Equal(t, &zero, ad.GetCodeHash())
}

// leftPad32 returns a 32-byte slice with b right-aligned (big-endian style).
func leftPad32(b []byte) []byte {
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// toArray32 converts a []byte to a *[32]byte (len must be 32).
func toArray32(b []byte) *[32]byte {
	return (*[32]byte)(b)
}

func toBalance(b []byte) *Balance {
	return (*Balance)(b)
}

func toCodeHash(b []byte) *CodeHash {
	return (*CodeHash)(b)
}
