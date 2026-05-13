package wireguard_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
)

// appendBytesField is shorthand for encoding `field N: BytesType payload`.
func appendBytesField(b []byte, num wireguard.Number, payload []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	b = protowire.AppendVarint(b, uint64(len(payload)))
	return append(b, payload...)
}

func TestScan_NilSchema(t *testing.T) {
	require.NoError(t, wireguard.Scan([]byte{0x01, 0x02}, nil))
}

func TestScan_EnforcesMaxCount(t *testing.T) {
	schema := &wireguard.Schema{
		Name: "Outer",
		Rules: map[wireguard.Number]wireguard.Rule{
			3: {MaxCount: 2},
		},
	}
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 3, nil)
	}
	err := wireguard.Scan(bz, schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Outer")
	require.Contains(t, err.Error(), "exceeds max 2")
}

func TestScan_MaxCountAtBoundary(t *testing.T) {
	schema := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 5}},
	}
	var bz []byte
	for i := 0; i < 5; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, wireguard.Scan(bz, schema))
}

func TestScan_DescendsIntoNested(t *testing.T) {
	inner := &wireguard.Schema{
		Name:  "Inner",
		Rules: map[wireguard.Number]wireguard.Rule{7: {MaxCount: 1}},
	}
	outer := &wireguard.Schema{
		Name:  "Outer",
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: inner}},
	}
	// Outer has one field 2 holding an Inner with two field-7s -> error.
	innerBytes := appendBytesField(nil, 7, nil)
	innerBytes = appendBytesField(innerBytes, 7, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	err := wireguard.Scan(bz, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Inner")
}

func TestScan_CountsAccumulateAcrossInstances(t *testing.T) {
	// MaxCount caps total occurrences across all instances of the enclosing
	// schema reached during the scan — not per-instance. Two outer fields
	// each carrying two inners hits four inner counts, which exceeds an
	// inner cap of 3 even though no single outer carries more than two.
	inner := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 3}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: inner, MaxCount: 5}},
	}
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	err := wireguard.Scan(bz, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max 3")
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {MaxCount: 1}},
	}
	// Field 1 is not in the schema; appearing many times should be fine.
	var bz []byte
	for i := 0; i < 100; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	bz = appendBytesField(bz, 3, nil)
	require.NoError(t, wireguard.Scan(bz, schema))
}

func TestScan_RejectsMalformedTag(t *testing.T) {
	// 0xff alone is a truncated varint.
	require.Error(t, wireguard.Scan([]byte{0xff}, &wireguard.Schema{}))
}

func TestScan_RejectsTruncatedLengthDelimited(t *testing.T) {
	bz := protowire.AppendTag(nil, 3, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't follow
	err := wireguard.Scan(bz, &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {MaxCount: 1}},
	})
	require.Error(t, err)
}

func TestScan_SkipsNonBytesFields(t *testing.T) {
	// A varint field (wire type 0) at position 5 should be walked past
	// without triggering any rule.
	bz := protowire.AppendTag(nil, 5, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 42)
	require.NoError(t, wireguard.Scan(bz, &wireguard.Schema{}))
}

// proto struct stand-in for MustFieldNum's reflection path; we don't pull in
// real generated types here because that would create a test-only dep on a
// proto package outside this package's purview.
type fixtureProto struct {
	Height int64 `protobuf:"varint,1,opt,name=height,proto3"`
	Sigs   []int `protobuf:"bytes,4,rep,name=signatures,proto3"`
}

func TestMustFieldNum_Resolves(t *testing.T) {
	require.Equal(t, protowire.Number(1), wireguard.MustFieldNum((*fixtureProto)(nil), "height"))
	require.Equal(t, protowire.Number(4), wireguard.MustFieldNum((*fixtureProto)(nil), "signatures"))
}

func TestMustFieldNum_PanicsOnUnknownField(t *testing.T) {
	require.PanicsWithValue(t,
		`wireguard: proto field "nope" not found on fixtureProto`,
		func() { wireguard.MustFieldNum((*fixtureProto)(nil), "nope") })
}

func TestScan_DuplicateNonRepeatedMessageCaughtByLeafCap(t *testing.T) {
	// Two duplicate occurrences of an enclosing message, each carrying inner
	// field-1 entries within the cap, should be caught because the inner
	// counter accumulates across the duplicates.
	inner := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 3}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: inner}},
	}
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	err := wireguard.Scan(bz, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max 3")
}

func TestScan_DistinctSchemasShareNoCounter(t *testing.T) {
	// Two different Schemas reached during the same Scan should each get
	// their own counter, even if they happen to use the same field number.
	leafA := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 2}},
	}
	leafB := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 2}},
	}
	root := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			2: {Nested: leafA},
			3: {Nested: leafB},
		},
	}
	a := appendBytesField(nil, 1, nil)
	a = appendBytesField(a, 1, nil)
	b := appendBytesField(nil, 1, nil)
	b = appendBytesField(b, 1, nil)
	bz := appendBytesField(nil, 2, a)
	bz = appendBytesField(bz, 3, b)
	// Each leaf hit twice, both within their own cap. Should pass.
	require.NoError(t, wireguard.Scan(bz, root))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	// MaxCount on a {Nested, MaxCount} rule caps how many times the field
	// itself may appear across the whole scan.
	inner := &wireguard.Schema{}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {Nested: inner, MaxCount: 3}},
	}
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 1, nil)
	}
	require.NoError(t, wireguard.Scan(bz, outer))
	bz = appendBytesField(bz, 1, nil)
	require.Error(t, wireguard.Scan(bz, outer))
}

func TestScan_DeepNestingBoundedCorrectly(t *testing.T) {
	// Smoke-test that nested Schema chains apply correctly: cap a leaf at
	// depth 3 and confirm an over-cap payload is rejected.
	leaf := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 2}},
	}
	mid := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: leaf}},
	}
	root := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {Nested: mid}},
	}
	leafBz := appendBytesField(nil, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	midBz := appendBytesField(nil, 2, leafBz)
	bz := appendBytesField(nil, 3, midBz)
	err := wireguard.Scan(bz, root)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "exceeds"))
}
