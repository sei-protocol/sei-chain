package common

import (
	"encoding/binary"
	"reflect"
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

func TestDecodePayloadBytes_Honest(t *testing.T) {
	// A single string contributes its own length.
	strArgs := abi.Arguments{{Type: mustABIType(t, "string", nil)}}
	data, err := strArgs.Pack("hello")
	require.NoError(t, err)
	n, ok := decodePayloadBytes(strArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("hello")), n)

	// A bytes value contributes its own length.
	bytesArgs := abi.Arguments{{Type: mustABIType(t, "bytes", nil)}}
	bytesPayload := []byte("a reasonably long bytes payload")
	data, err = bytesArgs.Pack(bytesPayload)
	require.NoError(t, err)
	n, ok = decodePayloadBytes(bytesArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len(bytesPayload)), n)

	// string[] sums the lengths of its elements.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	data, err = arrArgs.Pack([]string{"aa", "bbb", "c"})
	require.NoError(t, err)
	n, ok = decodePayloadBytes(arrArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("aa")+len("bbb")+len("c")), n)
}

func TestDecodePayloadBytes_Tuple(t *testing.T) {
	// Mirrors wasmd execute_batch((string,bytes,bytes)[]): every dynamic field
	// contributes its referenced payload length.
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
	n, ok := decodePayloadBytes(tupleArrArgs, data)
	require.True(t, ok)
	require.Equal(t, uint64(len("contract-one")+len("ignored")+len("ignored too")+len("two")+len("x")+len("y")), n)
}

// TestDecodePayloadBytes_Aliased verifies that every logical reference to a
// shared dynamic payload is counted.
func TestDecodePayloadBytes_Aliased(t *testing.T) {
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
	n, ok := decodePayloadBytes(arrArgs, data)
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

func TestDecodePayloadBytes_AliasedExecuteBatch(t *testing.T) {
	const (
		k = uint64(4)
		s = uint64(64)
	)
	args := abi.Arguments{{Type: mustABIType(t, "tuple[]", []abi.ArgumentMarshaling{
		{Name: "contractAddress", Type: "string"},
		{Name: "msg", Type: "bytes"},
		{Name: "coins", Type: "bytes"},
	})}}
	data := newAliasedExecuteBatchArgs(k, s)

	n, ok := decodePayloadBytes(args, data)
	require.True(t, ok)
	require.Equal(t, 3*k*s, n)

	vals, err := args.Unpack(data)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	require.Equal(t, int(k), reflect.ValueOf(vals[0]).Len())
}

// newAliasedExecuteBatchArgs builds a tuple array whose element offsets all
// reference one tuple body. The tuple's string and two bytes fields also share
// one payload.
func newAliasedExecuteBatchArgs(k, s uint64) []byte {
	tupleOffset := 32 * k
	arrayPayload := make([]byte, 0, int(tupleOffset+128+s))
	for range k {
		arrayPayload = append(arrayPayload, abiWord(tupleOffset)...)
	}
	for range 3 {
		arrayPayload = append(arrayPayload, abiWord(96)...)
	}
	arrayPayload = append(arrayPayload, abiWord(s)...)
	arrayPayload = append(arrayPayload, make([]byte, s)...)

	data := append(abiWord(32), abiWord(k)...)
	return append(data, arrayPayload...)
}

func TestDecodePayloadBytes_AliasedVoteWeighted(t *testing.T) {
	const (
		k = uint64(18432)
		s = uint64(602592)
	)
	args := abi.Arguments{
		{Type: mustABIType(t, "uint64", nil)},
		{Type: mustABIType(t, "tuple[]", []abi.ArgumentMarshaling{
			{Name: "option", Type: "int32"},
			{Name: "weight", Type: "string"},
		})},
	}
	data := newAliasedVoteWeightedArgs(k, s)

	n, ok := decodePayloadBytes(args, data)
	require.True(t, ok)
	require.Equal(t, k*s, n)

	// DecodeGasCost must price the amplified copy volume, not just len(input).
	input := append([]byte{0x59, 0x63, 0x4a, 0x88}, data...)
	gas, ok := DecodeGasCost(args, input)
	require.True(t, ok)
	base := DefaultGasCost(input, false)
	require.Equal(t, satAdd(base, satMul(storetypes.KVGasConfig().ReadCostPerByte, k*s)), gas)
	require.Greater(t, gas, uint64(12_500_000))
	require.Less(t, uint64(len(input)), uint64(2<<20)) // compact calldata, ~1.2MiB
}

// newAliasedVoteWeightedArgs builds non-canonical voteWeighted argument
// bytes: k dynamic-tuple offsets all point at one shared (option, weight) body
// whose weight string has length s. Calldata stays O(k+s); decoded copy volume
// is O(k*s).
func newAliasedVoteWeightedArgs(k, s uint64) []byte {
	tupleRel := 32 * k // offset within the array payload (after the length word)
	arrayPayload := make([]byte, 0, int(32*k+64+s))
	for range k {
		arrayPayload = append(arrayPayload, abiWord(tupleRel)...)
	}
	// Shared dynamic tuple: word0 = s (option / string length via offset 0),
	// word1 = 0 (string offset), then s payload bytes starting at tuple+32.
	arrayPayload = append(arrayPayload, abiWord(s)...)
	arrayPayload = append(arrayPayload, abiWord(0)...)
	arrayPayload = append(arrayPayload, make([]byte, s)...)

	data := append(abiWord(1), abiWord(64)...) // proposalID, options offset
	data = append(data, abiWord(k)...)
	data = append(data, arrayPayload...)
	return data
}

func TestDecodePayloadBytes_Malformed(t *testing.T) {
	// A string[] header claiming an offset past the end of the buffer must not
	// be scanned as if valid.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	data := append(abiWord(64), abiWord(1)...) // offset 64 into a 64-byte buffer
	_, ok := decodePayloadBytes(arrArgs, data)
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

	// bytes arg: base over the whole input plus the referenced payload volume.
	bytesArgs := abi.Arguments{{Type: mustABIType(t, "bytes", nil)}}
	bytesPayload := []byte("hello bytes")
	packed, err = bytesArgs.Pack(bytesPayload)
	require.NoError(t, err)
	input = append([]byte{0xaa, 0xbb, 0xcc, 0xdd}, packed...)
	want = satAdd(DefaultGasCost(input, false), satMul(perByte, uint64(len(bytesPayload))))
	gas, ok = DecodeGasCost(bytesArgs, input)
	require.True(t, ok)
	require.Equal(t, want, gas)

	// Structurally invalid calldata: reported as not-ok so the caller rejects it.
	arrArgs := abi.Arguments{{Type: mustABIType(t, "string[]", nil)}}
	bad := append([]byte{0xaa, 0xbb, 0xcc, 0xdd}, append(abiWord(64), abiWord(1)...)...)
	_, ok = DecodeGasCost(arrArgs, bad)
	require.False(t, ok)
}
