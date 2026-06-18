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
	// MaxCount is checked per instance of the containing message, not summed
	// globally across all occurrences. This keeps caps easy to reason about:
	// "at most N per outer element" rather than "N total across the payload".
	// Memory is still bounded at every level by the product of the caps along
	// the nesting path (outer_cap × inner_cap × …).
	//
	// Concretely: outer.field2 cap=5, inner.field1 cap=3.
	// Five outer instances each with three inners is fine (5×3=15 total items
	// but each instance is within its own cap). A single instance with four
	// inners exceeds its per-instance cap and must be rejected.
	inner := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{1: {MaxCount: 3}},
	}
	outer := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{2: {Nested: utils.Some(inner), MaxCount: 5}},
	}

	threeInners := appendBytesField(nil, 1, nil)
	threeInners = appendBytesField(threeInners, 1, nil)
	threeInners = appendBytesField(threeInners, 1, nil)

	// Five outer instances each at the inner cap: all pass.
	var bz []byte
	for range 5 {
		bz = appendBytesField(bz, 2, threeInners)
	}
	require.NoError(t, wireguard.Scan(bz, outer))

	// One outer instance with four inners exceeds the per-instance cap.
	fourInners := appendBytesField(threeInners, 1, nil)
	bz2 := appendBytesField(nil, 2, threeInners)
	bz2 = appendBytesField(bz2, 2, fourInners)
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
