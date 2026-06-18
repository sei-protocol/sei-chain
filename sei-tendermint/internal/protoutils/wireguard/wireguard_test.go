package wireguard_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appendBytesField is shorthand for encoding `field N: BytesType payload`.
func appendBytesField(b []byte, num wireguard.Number, payload []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	b = protowire.AppendVarint(b, uint64(len(payload)))
	return append(b, payload...)
}

func TestScan_NilSchema(t *testing.T) {
	var schema *wireguard.Schema
	require.NoError(t, schema.Scan([]byte{0x01, 0x02}))
}

func TestScan_EnforcesMaxCount(t *testing.T) {
	schema := &wireguard.Schema{3: {MaxCount: 2}}
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 3, nil)
	}
	require.Error(t, schema.Scan(bz))
}

func TestScan_MaxCountAtBoundary(t *testing.T) {
	schema := &wireguard.Schema{1: {MaxCount: 5}}
	var bz []byte
	for i := 0; i < 5; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, schema.Scan(bz))
}

func TestScan_DescendsIntoNested(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	inner := &wireguard.Schema{7: {MaxCount: 1}}
	wireguard.MustRegister[*innerMsg](inner)
	outer := &wireguard.Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]())}}
	wireguard.MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 7, nil)
	innerBytes = appendBytesField(innerBytes, 7, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	require.Error(t, wireguard.Scan[*outerMsg](bz))
}

func TestScan_CountsAccumulateAcrossInstances(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// MaxCount caps total occurrences across all instances of the enclosing
	// schema reached during the scan — not per-instance. Two outer fields
	// each carrying two inners hits four inner counts, which exceeds an
	// inner cap of 3 even though no single outer carries more than two.
	inner := &wireguard.Schema{1: {MaxCount: 3}}
	wireguard.MustRegister[*innerMsg](inner)
	outer := &wireguard.Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]()), MaxCount: 5}}
	wireguard.MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.Error(t, wireguard.Scan[*outerMsg](bz))
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := &wireguard.Schema{3: {MaxCount: 1}}
	var bz []byte
	for i := 0; i < 100; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	bz = appendBytesField(bz, 3, nil)
	require.NoError(t, schema.Scan(bz))
}

func TestScan_RejectsMalformedTag(t *testing.T) {
	// 0xff alone is a truncated varint.
	require.Error(t, (&wireguard.Schema{}).Scan([]byte{0xff}))
}

func TestScan_RejectsTruncatedLengthDelimited(t *testing.T) {
	bz := protowire.AppendTag(nil, 3, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't follow
	err := (&wireguard.Schema{3: {MaxCount: 1}}).Scan(bz)
	require.Error(t, err)
}

func TestScan_SkipsNonBytesFields(t *testing.T) {
	bz := protowire.AppendTag(nil, 5, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 42)
	require.NoError(t, (&wireguard.Schema{}).Scan(bz))
}

func TestScan_DuplicateNonRepeatedMessageCaughtByLeafCap(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	// Two duplicate occurrences of an enclosing message, each carrying inner
	// field-1 entries within the cap, should be caught because the inner
	// counter accumulates across the duplicates.
	inner := &wireguard.Schema{1: {MaxCount: 3}}
	wireguard.MustRegister[*innerMsg](inner)
	outer := &wireguard.Schema{2: {Nested: utils.Some(reflect.TypeFor[*innerMsg]())}}
	wireguard.MustRegister[*outerMsg](outer)
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.Error(t, wireguard.Scan[*outerMsg](bz))
}

func TestScan_DistinctSchemasShareNoCounter(t *testing.T) {
	type leafAMsg struct{}
	type leafBMsg struct{}
	type rootMsg struct{}
	// Two different Schemas reached during the same Scan should each get
	// their own counter, even if they happen to use the same field number.
	leafA := &wireguard.Schema{1: {MaxCount: 2}}
	wireguard.MustRegister[*leafAMsg](leafA)
	leafB := &wireguard.Schema{1: {MaxCount: 2}}
	wireguard.MustRegister[*leafBMsg](leafB)
	root := &wireguard.Schema{
		2: {Nested: utils.Some(reflect.TypeFor[*leafAMsg]())},
		3: {Nested: utils.Some(reflect.TypeFor[*leafBMsg]())},
	}
	wireguard.MustRegister[*rootMsg](root)
	a := appendBytesField(nil, 1, nil)
	a = appendBytesField(a, 1, nil)
	b := appendBytesField(nil, 1, nil)
	b = appendBytesField(b, 1, nil)
	bz := appendBytesField(nil, 2, a)
	bz = appendBytesField(bz, 3, b)
	require.NoError(t, wireguard.Scan[*rootMsg](bz))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	type innerMsg struct{}
	type outerMsg struct{}
	inner := &wireguard.Schema{}
	wireguard.MustRegister[*innerMsg](inner)
	outer := &wireguard.Schema{1: {Nested: utils.Some(reflect.TypeFor[*innerMsg]()), MaxCount: 3}}
	wireguard.MustRegister[*outerMsg](outer)
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, wireguard.Scan[*outerMsg](bz))
	bz = appendBytesField(bz, 1, nil)
	require.Error(t, wireguard.Scan[*outerMsg](bz))
}

func TestScan_DeepNestingBoundedCorrectly(t *testing.T) {
	type leafMsg struct{}
	type midMsg struct{}
	type rootMsg struct{}
	leaf := &wireguard.Schema{1: {MaxCount: 2}}
	wireguard.MustRegister[*leafMsg](leaf)
	mid := &wireguard.Schema{2: {Nested: utils.Some(reflect.TypeFor[*leafMsg]())}}
	wireguard.MustRegister[*midMsg](mid)
	root := &wireguard.Schema{3: {Nested: utils.Some(reflect.TypeFor[*midMsg]())}}
	wireguard.MustRegister[*rootMsg](root)
	leafBz := appendBytesField(nil, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	midBz := appendBytesField(nil, 2, leafBz)
	bz := appendBytesField(nil, 3, midBz)
	require.Error(t, wireguard.Scan[*rootMsg](bz))
}
