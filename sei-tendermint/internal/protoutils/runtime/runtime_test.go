package runtime

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// appendBytesField is shorthand for encoding `field N: BytesType payload`.
func appendBytesField(b []byte, num Number, payload []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	b = protowire.AppendVarint(b, uint64(len(payload)))
	return append(b, payload...)
}

func TestScan_NilSchema(t *testing.T) {
	var schema Schema
	require.NoError(t, schema.scan([]byte{0x01, 0x02}))
}

func TestScan_EnforcesMaxCount(t *testing.T) {
	schema := Schema{3: {MaxCount: 2}}
	var bz []byte
	for range 3 {
		bz = appendBytesField(bz, 3, nil)
	}
	require.Error(t, schema.scan(bz))
}

func TestScan_MaxCountAtBoundary(t *testing.T) {
	schema := Schema{1: {MaxCount: 5}}
	var bz []byte
	for range 5 {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, schema.scan(bz))
}

func TestScan_EnforcesMaxSize(t *testing.T) {
	schema := Schema{3: {MaxSize: 2}}
	bz := appendBytesField(nil, 3, []byte{1, 2, 3})
	require.Error(t, schema.scan(bz))
}

func TestScan_MaxSizeAtBoundary(t *testing.T) {
	schema := Schema{3: {MaxSize: 3}}
	bz := appendBytesField(nil, 3, []byte{1, 2, 3})
	require.NoError(t, schema.scan(bz))
}

func TestScan_EnforcesMaxTotalSize(t *testing.T) {
	schema := Schema{3: {MaxTotalSize: 5}}
	var bz []byte
	bz = appendBytesField(bz, 3, []byte{1, 2})
	bz = appendBytesField(bz, 3, []byte{3, 4, 5, 6})
	require.Error(t, schema.scan(bz))
}

func TestScan_MaxTotalSizeAtBoundary(t *testing.T) {
	schema := Schema{3: {MaxTotalSize: 5}}
	var bz []byte
	bz = appendBytesField(bz, 3, []byte{1, 2})
	bz = appendBytesField(bz, 3, []byte{3, 4, 5})
	require.NoError(t, schema.scan(bz))
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := Schema{3: {MaxCount: 1}}
	var bz []byte
	for range 100 {
		bz = appendBytesField(bz, 1, nil)
	}
	bz = appendBytesField(bz, 3, nil)
	require.NoError(t, schema.scan(bz))
}

func TestScan_RejectsMalformedTag(t *testing.T) {
	// 0xff alone is a truncated varint.
	schema := Schema{1: {MaxCount: 1}}
	require.Error(t, schema.scan([]byte{0xff}))
}

func TestScan_RejectsTruncatedLengthDelimited(t *testing.T) {
	bz := protowire.AppendTag(nil, 3, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't follow
	schema := Schema{3: {MaxCount: 1}}
	require.Error(t, schema.scan(bz))
}

func TestScan_SkipsNonBytesFields(t *testing.T) {
	bz := protowire.AppendTag(nil, 5, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 42)
	schema := Schema{}
	require.NoError(t, schema.scan(bz))
}
