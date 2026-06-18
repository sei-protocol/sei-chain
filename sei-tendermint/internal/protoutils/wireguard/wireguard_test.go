package wireguard

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 3, nil)
	}
	require.Error(t, schema.scan(bz))
}

func TestScan_MaxCountAtBoundary(t *testing.T) {
	schema := Schema{1: {MaxCount: 5}}
	var bz []byte
	for i := 0; i < 5; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, schema.scan(bz))
}

func TestScan_DescendsIntoNested(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	inner := Schema{7: {MaxCount: 1}}
	MustRegister[*innerMsg](inner)
	outer := Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]())}}
	MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 7, nil)
	innerBytes = appendBytesField(innerBytes, 7, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	require.Error(t, Scan[*outerMsg](bz))
}

func TestScan_CountsResetAcrossInstances(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// Each nested message instance gets its own counter set, so two sibling
	// messages carrying under-cap children should both pass.
	inner := Schema{1: {MaxCount: 3}}
	MustRegister[*innerMsg](inner)
	outer := Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]()), MaxCount: 5}}
	MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.NoError(t, Scan[*outerMsg](bz))
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := Schema{3: {MaxCount: 1}}
	var bz []byte
	for i := 0; i < 100; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	bz = appendBytesField(bz, 3, nil)
	require.NoError(t, schema.scan(bz))
}

func TestScan_RejectsMalformedTag(t *testing.T) {
	// 0xff alone is a truncated varint.
	schema := Schema{}
	require.Error(t, schema.scan([]byte{0xff}))
}

func TestScan_RejectsTruncatedLengthDelimited(t *testing.T) {
	bz := protowire.AppendTag(nil, 3, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't follow
	schema := Schema{3: {MaxCount: 1}}
	err := schema.scan(bz)
	require.Error(t, err)
}

func TestScan_SkipsNonBytesFields(t *testing.T) {
	bz := protowire.AppendTag(nil, 5, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 42)
	schema := Schema{}
	require.NoError(t, schema.scan(bz))
}

func TestScan_DuplicateNonRepeatedMessagesGetSeparateChildBudgets(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// Duplicate wrapper fields still produce independent nested message
	// instances, so each child gets its own budget.
	inner := Schema{1: {MaxCount: 3}}
	MustRegister[*innerMsg](inner)
	outer := Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]())}}
	MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.NoError(t, Scan[*outerMsg](bz))
}

func TestScan_DistinctSchemasStayIndependent(t *testing.T) {
	type leafAMsg struct{}
	type leafBMsg struct{}
	type rootMsg struct{}
	// Different nested message instances stay independent even if they use
	// the same field number.
	leafA := Schema{1: {MaxCount: 2}}
	MustRegister[*leafAMsg](leafA)
	leafB := Schema{1: {MaxCount: 2}}
	MustRegister[*leafBMsg](leafB)
	root := Schema{
		2: {Nested: utils.Some(reflect.TypeFor[*leafAMsg]())},
		3: {Nested: utils.Some(reflect.TypeFor[*leafBMsg]())},
	}
	MustRegister[*rootMsg](root)
	a := appendBytesField(nil, 1, nil)
	a = appendBytesField(a, 1, nil)
	b := appendBytesField(nil, 1, nil)
	b = appendBytesField(b, 1, nil)
	bz := appendBytesField(nil, 2, a)
	bz = appendBytesField(bz, 3, b)
	require.NoError(t, Scan[*rootMsg](bz))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	inner := Schema{}
	MustRegister[*innerMsg](inner)
	outer := Schema{1: {Nested: utils.Some(reflect.TypeFor[*innerMsg]()), MaxCount: 3}}
	MustRegister[*outerMsg](outer)
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, Scan[*outerMsg](bz))
	bz = appendBytesField(bz, 1, nil)
	require.Error(t, Scan[*outerMsg](bz))
}

func TestScan_DeepNestingBoundedCorrectly(t *testing.T) {
	type leafMsg struct{}
	type midMsg struct{}
	type rootMsg struct{}
	leaf := Schema{1: {MaxCount: 2}}
	MustRegister[*leafMsg](leaf)
	mid := Schema{2: {Nested: utils.Some(reflect.TypeFor[*leafMsg]())}}
	MustRegister[*midMsg](mid)
	root := Schema{3: {Nested: utils.Some(reflect.TypeFor[*midMsg]())}}
	MustRegister[*rootMsg](root)
	leafBz := appendBytesField(nil, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	midBz := appendBytesField(nil, 2, leafBz)
	bz := appendBytesField(nil, 3, midBz)
	require.Error(t, Scan[*rootMsg](bz))
}
