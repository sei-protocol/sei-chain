package common

import (
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/stretchr/testify/require"
)

func mustABIType(t *testing.T, sol string, components []abi.ArgumentMarshaling) abi.Type {
	t.Helper()
	typ, err := abi.NewType(sol, "", components)
	require.NoError(t, err)
	return typ
}

func abiWord(v uint64) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint64(b[24:], v)
	return b
}

func TestDecodeStringCopyBytes_Honest(t *testing.T) {
	// A single string contributes its own length.
	strArgs := abi.Arguments{{Type: mustABIType(t, "string", nil)}}
	data, err := strArgs.Pack("hello")
	require.NoError(t, err)
	n, ok := decodeStringCopyBytes(strArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("hello")), n)

	// bytes are resliced by the decoder, not copied, so they cost nothing here.
	bytesArgs := abi.Arguments{{Type: mustABIType(t, "bytes", nil)}}
	data, err = bytesArgs.Pack([]byte("a reasonably long bytes payload"))
	require.NoError(t, err)
	n, ok = decodeStringCopyBytes(bytesArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(0), n)

	// string[] sums the lengths of its elements.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	data, err = arrArgs.Pack([]string{"aa", "bbb", "c"})
	require.NoError(t, err)
	n, ok = decodeStringCopyBytes(arrArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("aa")+len("bbb")+len("c")), n)
}

func TestDecodeStringCopyBytes_Tuple(t *testing.T) {
	// Mirrors wasmd execute_batch((string,bytes,bytes)[]): only the string field
	// of each tuple is copied; the bytes fields are resliced.
	tupleArrArgs := abi.Arguments{{Type: mustABIType(t, "tuple[]", []abi.ArgumentMarshaling{
		{Name: "contractAddress", Type: "string"},
		{Name: "msg", Type: "bytes"},
		{Name: "coins", Type: "bytes"},
	})}}
	type execMsg struct {
		ContractAddress string `abi:"contractAddress"`
		Msg             []byte `abi:"msg"`
		Coins           []byte `abi:"coins"`
	}
	data, err := tupleArrArgs.Pack([]execMsg{
		{ContractAddress: "contract-one", Msg: []byte("ignored"), Coins: []byte("ignored too")},
		{ContractAddress: "two", Msg: []byte("x"), Coins: []byte("y")},
	})
	require.NoError(t, err)
	n, ok := decodeStringCopyBytes(tupleArrArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("contract-one")+len("two")), n)
}

// TestDecodeStringCopyBytes_Aliased is the core case: an attacker-crafted
// string[] whose K element offsets all point at the same S-byte string. The
// decoder copies K*S bytes even though the input is only ~(32*K + S) bytes, so
// the copied volume is super-linear in len(input). The estimator must report the
// full K*S so the caller is charged for the real work — while itself running in
// O(K), not O(K*S).
func TestDecodeStringCopyBytes_Aliased(t *testing.T) {
	const (
		k = uint64(4)
		s = uint64(64)
	)
	// sub = element data region (what the decoder addresses relative to it):
	//   [k head words][length word = s][s bytes of payload]
	headTarget := 32 * k // offset within sub of the shared length word
	sub := make([]byte, 0, headTarget+32+s)
	for range k {
		sub = append(sub, abiWord(headTarget)...) // every element points to the same string
	}
	sub = append(sub, abiWord(s)...)
	sub = append(sub, make([]byte, s)...)

	// data = [offset to array data = 32][array length = k][sub]
	data := append(abiWord(32), abiWord(k)...)
	data = append(data, sub...)

	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}

	// The estimator reports the full aliased copy volume.
	n, ok := decodeStringCopyBytes(arrArgs, data)
	require.True(t, ok)
	require.Equal(t, k*s, n)

	// Sanity check against the real decoder: it accepts the encoding and
	// materializes k strings of length s each (i.e. it really does copy k*s).
	vals, err := arrArgs.Unpack(data)
	require.NoError(t, err)
	strs := vals[0].([]string)
	require.Len(t, strs, int(k))
	for _, str := range strs {
		require.Equal(t, int(s), len(str))
	}
}

func TestDecodeStringCopyBytes_Malformed(t *testing.T) {
	// A string[] header claiming an offset past the end of the buffer must not
	// be scanned as if valid.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	data := append(abiWord(64), abiWord(1)...) // offset 64 into a 64-byte buffer
	_, ok := decodeStringCopyBytes(arrArgs, data)
	require.False(t, ok)
}

func TestDecodeGasCost(t *testing.T) {
	perByte := storetypes.KVGasConfig().ReadCostPerByte

	// No-arg selector: just the linear base.
	noArgs := abi.Arguments{}
	input := []byte{0x01, 0x02, 0x03, 0x04}
	gas, ok := DecodeGasCost(noArgs, input)
	require.True(t, ok)
	require.Equal(t, DefaultGasCost(input, false), gas)

	// string arg: base over the whole input plus the string-copy volume.
	strArgs := abi.Arguments{{Type: mustABIType(t, "string", nil)}}
	packed, err := strArgs.Pack("hello world")
	require.NoError(t, err)
	input = append([]byte{0xaa, 0xbb, 0xcc, 0xdd}, packed...)
	want := satAdd(DefaultGasCost(input, false), satMul(perByte, uint64(len("hello world"))))
	gas, ok = DecodeGasCost(strArgs, input)
	require.True(t, ok)
	require.Equal(t, want, gas)
	require.Greater(t, gas, DefaultGasCost(input, false))

	// Structurally invalid calldata: reported as not-ok so the caller rejects it.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	bad := append([]byte{0xaa, 0xbb, 0xcc, 0xdd}, append(abiWord(64), abiWord(1)...)...)
	_, ok = DecodeGasCost(arrArgs, bad)
	require.False(t, ok)
}
