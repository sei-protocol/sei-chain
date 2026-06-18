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
	require.NoError(t, schema.scan([]byte{0x01, 0x02}, &schema, map[counterKey]int{}))
}

func TestScan_EnforcesMaxCount(t *testing.T) {
	schema := Schema{3: {MaxCount: 2}}
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 3, nil)
	}
	require.Error(t, schema.scan(bz, &schema, map[counterKey]int{}))
}

func TestScan_MaxCountAtBoundary(t *testing.T) {
	schema := Schema{1: {MaxCount: 5}}
	var bz []byte
	for i := 0; i < 5; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, schema.scan(bz, &schema, map[counterKey]int{}))
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

func TestScan_CountsAccumulateAcrossInstances(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// MaxCount caps total occurrences across all instances of the enclosing
	// schema reached during the scan — not per-instance. Two outer fields
	// each carrying two inners hits four inner counts, which exceeds an
	// inner cap of 3 even though no single outer carries more than two.
	inner := Schema{1: {MaxCount: 3}}
	MustRegister[*innerMsg](inner)
	outer := Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]()), MaxCount: 5}}
	MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.Error(t, Scan[*outerMsg](bz))
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := Schema{3: {MaxCount: 1}}
	var bz []byte
	for i := 0; i < 100; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	bz = appendBytesField(bz, 3, nil)
	require.NoError(t, schema.scan(bz, &schema, map[counterKey]int{}))
}

func TestScan_RejectsMalformedTag(t *testing.T) {
	// 0xff alone is a truncated varint.
	schema := Schema{}
	require.Error(t, schema.scan([]byte{0xff}, &schema, map[counterKey]int{}))
}

func TestScan_RejectsTruncatedLengthDelimited(t *testing.T) {
	bz := protowire.AppendTag(nil, 3, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't follow
	schema := Schema{3: {MaxCount: 1}}
	err := schema.scan(bz, &schema, map[counterKey]int{})
	require.Error(t, err)
}

func TestScan_SkipsNonBytesFields(t *testing.T) {
	bz := protowire.AppendTag(nil, 5, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 42)
	schema := Schema{}
	require.NoError(t, schema.scan(bz, &schema, map[counterKey]int{}))
}

func TestScan_DuplicateNonRepeatedMessageCaughtByLeafCap(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// Two duplicate occurrences of an enclosing message, each carrying inner
	// field-1 entries within the cap, should be caught because the inner
	// counter accumulates across the duplicates.
	inner := Schema{1: {MaxCount: 3}}
	MustRegister[*innerMsg](inner)
	outer := Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]())}}
	MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.Error(t, Scan[*outerMsg](bz))
}

func TestScan_DistinctSchemasShareNoCounter(t *testing.T) {
	type leafAMsg struct{}
	type leafBMsg struct{}
	type rootMsg struct{}
	// Two different Schemas reached during the same Scan should each get
	// their own counter, even if they happen to use the same field number.
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
