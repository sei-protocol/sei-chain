package wireguard_test

import (
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
	require.NoError(t, wireguard.Scan([]byte{0x01, 0x02}, nil))
}

func TestScan_EnforcesMaxCount(t *testing.T) {
	schema := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {MaxCount: 2}},
	}
	var bz []byte
	for i := 0; i < 3; i++ {
		bz = appendBytesField(bz, 3, nil)
	}
	require.Error(t, wireguard.Scan(bz, schema))
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
		Rules: map[wireguard.Number]wireguard.Rule{7: {MaxCount: 1}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: utils.Some(inner)}},
	}
	innerBytes := appendBytesField(nil, 7, nil)
	innerBytes = appendBytesField(innerBytes, 7, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	require.Error(t, wireguard.Scan(bz, outer))
}

func TestScan_CountsArePerNestedInstance(t *testing.T) {
	// MaxCount on an inner field is checked per occurrence of the outer
	// message, not summed globally. Two outer instances each carrying two
	// inners (cap=3) must both pass; only a single instance that exceeds the
	// cap should be rejected.
	inner := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 3}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: utils.Some(inner), MaxCount: 5}},
	}
	twoInners := appendBytesField(nil, 1, nil)
	twoInners = appendBytesField(twoInners, 1, nil)
	bz := appendBytesField(nil, 2, twoInners)
	bz = appendBytesField(bz, 2, twoInners)
	// Two outer instances each with 2 inners (< 3): should pass.
	require.NoError(t, wireguard.Scan(bz, outer))

	fourInners := appendBytesField(nil, 1, nil)
	fourInners = appendBytesField(fourInners, 1, nil)
	fourInners = appendBytesField(fourInners, 1, nil)
	fourInners = appendBytesField(fourInners, 1, nil)
	bz2 := appendBytesField(nil, 2, twoInners)
	bz2 = appendBytesField(bz2, 2, fourInners) // second outer has 4 inners > 3
	// One outer instance exceeds the per-instance cap: should fail.
	require.Error(t, wireguard.Scan(bz2, outer))
}

func TestScan_IgnoresUnrelatedFields(t *testing.T) {
	schema := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {MaxCount: 1}},
	}
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
	require.Equal(t, protowire.Number(1), wireguard.MustFieldNum[fixtureProto]("height"))
	require.Equal(t, protowire.Number(4), wireguard.MustFieldNum[fixtureProto]("signatures"))
}

func TestMustFieldNum_PanicsOnUnknownField(t *testing.T) {
	require.PanicsWithValue(t,
		`wireguard: proto field "nope" not found on fixtureProto`,
		func() { wireguard.MustFieldNum[fixtureProto]("nope") })
}

func TestScan_DuplicateOuterEachWithinCapPasses(t *testing.T) {
	// Two occurrences of a nested field each carrying 2 inner items (cap=3)
	// must both pass — the cap is per-instance, not a global sum.
	inner := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 3}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: utils.Some(inner)}},
	}
	innerBytes := appendBytesField(nil, 1, nil)
	innerBytes = appendBytesField(innerBytes, 1, nil)
	bz := appendBytesField(nil, 2, innerBytes)
	bz = appendBytesField(bz, 2, innerBytes)
	require.NoError(t, wireguard.Scan(bz, outer))
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
			2: {Nested: utils.Some(leafA)},
			3: {Nested: utils.Some(leafB)},
		},
	}
	a := appendBytesField(nil, 1, nil)
	a = appendBytesField(a, 1, nil)
	b := appendBytesField(nil, 1, nil)
	b = appendBytesField(b, 1, nil)
	bz := appendBytesField(nil, 2, a)
	bz = appendBytesField(bz, 3, b)
	require.NoError(t, wireguard.Scan(bz, root))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	inner := &wireguard.Schema{}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {Nested: utils.Some(inner), MaxCount: 3}},
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
	leaf := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 2}},
	}
	mid := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: utils.Some(leaf)}},
	}
	root := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{3: {Nested: utils.Some(mid)}},
	}
	leafBz := appendBytesField(nil, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	leafBz = appendBytesField(leafBz, 1, nil)
	midBz := appendBytesField(nil, 2, leafBz)
	bz := appendBytesField(nil, 3, midBz)
	require.Error(t, wireguard.Scan(bz, root))
}
