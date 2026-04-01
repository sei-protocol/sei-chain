package vtype

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Constants and type sizes ---

func TestConstantValues(t *testing.T) {
	require.Equal(t, 20, AddressLen)
	require.Equal(t, 32, CodeHashLen)
	require.Equal(t, 8, NonceLen)
	require.Equal(t, 32, SlotLen)
	require.Equal(t, 32, BalanceLen)
}

func TestTypeSizes(t *testing.T) {
	require.Len(t, Address{}, AddressLen)
	require.Len(t, CodeHash{}, CodeHashLen)
	require.Len(t, Slot{}, SlotLen)
	require.Len(t, Balance{}, BalanceLen)
}

// --- ParseNonce ---

func TestParseNonce_Valid(t *testing.T) {
	buf := make([]byte, NonceLen)
	binary.BigEndian.PutUint64(buf, 42)
	n, err := ParseNonce(buf)
	require.NoError(t, err)
	require.Equal(t, uint64(42), n)
}

func TestParseNonce_Zero(t *testing.T) {
	n, err := ParseNonce(make([]byte, NonceLen))
	require.NoError(t, err)
	require.Equal(t, uint64(0), n)
}

func TestParseNonce_MaxUint64(t *testing.T) {
	buf := bytes.Repeat([]byte{0xff}, NonceLen)
	n, err := ParseNonce(buf)
	require.NoError(t, err)
	require.Equal(t, uint64(0xffffffffffffffff), n)
}

func TestParseNonce_TooShort(t *testing.T) {
	_, err := ParseNonce([]byte{0x01, 0x02})
	require.Error(t, err)
}

func TestParseNonce_TooLong(t *testing.T) {
	_, err := ParseNonce(make([]byte, NonceLen+1))
	require.Error(t, err)
}

func TestParseNonce_Empty(t *testing.T) {
	_, err := ParseNonce([]byte{})
	require.Error(t, err)
}

func TestParseNonce_Nil(t *testing.T) {
	_, err := ParseNonce(nil)
	require.Error(t, err)
}

// --- ParseCodeHash ---

func TestParseCodeHash_Valid(t *testing.T) {
	input := bytes.Repeat([]byte{0xab}, CodeHashLen)
	ch, err := ParseCodeHash(input)
	require.NoError(t, err)
	require.Equal(t, input, ch[:])
}

func TestParseCodeHash_Zero(t *testing.T) {
	ch, err := ParseCodeHash(make([]byte, CodeHashLen))
	require.NoError(t, err)
	var zero CodeHash
	require.Equal(t, &zero, ch)
}

func TestParseCodeHash_CopiesInput(t *testing.T) {
	input := bytes.Repeat([]byte{0xab}, CodeHashLen)
	ch, err := ParseCodeHash(input)
	require.NoError(t, err)
	input[0] = 0xff
	require.Equal(t, byte(0xab), ch[0], "ParseCodeHash must copy, not alias")
}

func TestParseCodeHash_TooShort(t *testing.T) {
	_, err := ParseCodeHash([]byte{0x01})
	require.Error(t, err)
}

func TestParseCodeHash_TooLong(t *testing.T) {
	_, err := ParseCodeHash(make([]byte, CodeHashLen+1))
	require.Error(t, err)
}

func TestParseCodeHash_Empty(t *testing.T) {
	_, err := ParseCodeHash([]byte{})
	require.Error(t, err)
}

func TestParseCodeHash_Nil(t *testing.T) {
	_, err := ParseCodeHash(nil)
	require.Error(t, err)
}

// --- ParseBalance ---

func TestParseBalance_Valid(t *testing.T) {
	input := leftPad32([]byte{0x01, 0x00})
	bal, err := ParseBalance(input)
	require.NoError(t, err)
	require.Equal(t, input, bal[:])
}

func TestParseBalance_Zero(t *testing.T) {
	bal, err := ParseBalance(make([]byte, BalanceLen))
	require.NoError(t, err)
	var zero Balance
	require.Equal(t, &zero, bal)
}

func TestParseBalance_CopiesInput(t *testing.T) {
	input := bytes.Repeat([]byte{0xab}, BalanceLen)
	bal, err := ParseBalance(input)
	require.NoError(t, err)
	input[0] = 0xff
	require.Equal(t, byte(0xab), bal[0], "ParseBalance must copy, not alias")
}

func TestParseBalance_TooShort(t *testing.T) {
	_, err := ParseBalance([]byte{0x01})
	require.Error(t, err)
}

func TestParseBalance_TooLong(t *testing.T) {
	_, err := ParseBalance(make([]byte, BalanceLen+1))
	require.Error(t, err)
}

func TestParseBalance_Empty(t *testing.T) {
	_, err := ParseBalance([]byte{})
	require.Error(t, err)
}

func TestParseBalance_Nil(t *testing.T) {
	_, err := ParseBalance(nil)
	require.Error(t, err)
}

// --- ParseStorageValue ---

func TestParseStorageValue_Valid(t *testing.T) {
	input := leftPad32([]byte{0xde, 0xad})
	val, err := ParseStorageValue(input)
	require.NoError(t, err)
	require.Equal(t, input, val[:])
}

func TestParseStorageValue_Zero(t *testing.T) {
	val, err := ParseStorageValue(make([]byte, SlotLen))
	require.NoError(t, err)
	var zero [32]byte
	require.Equal(t, &zero, val)
}

func TestParseStorageValue_CopiesInput(t *testing.T) {
	input := bytes.Repeat([]byte{0xab}, SlotLen)
	val, err := ParseStorageValue(input)
	require.NoError(t, err)
	input[0] = 0xff
	require.Equal(t, byte(0xab), val[0], "ParseStorageValue must copy, not alias")
}

func TestParseStorageValue_TooShort(t *testing.T) {
	_, err := ParseStorageValue([]byte{0x01})
	require.Error(t, err)
}

func TestParseStorageValue_TooLong(t *testing.T) {
	_, err := ParseStorageValue(make([]byte, SlotLen+1))
	require.Error(t, err)
}

func TestParseStorageValue_Empty(t *testing.T) {
	_, err := ParseStorageValue([]byte{})
	require.Error(t, err)
}

func TestParseStorageValue_Nil(t *testing.T) {
	_, err := ParseStorageValue(nil)
	require.Error(t, err)
}
