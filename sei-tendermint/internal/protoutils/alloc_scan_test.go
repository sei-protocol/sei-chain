package protoutils_test

import (
	"fmt"
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestUnmarshalWithLimit_SmallMessageAccepted verifies that a legitimate small
// message is accepted when the limit is generous.
func TestUnmarshalWithLimit_SmallMessageAccepted(t *testing.T) {
	msg := &pb.OuterNotSized{
		B: make([]*pb.SizedOk, 3),
	}
	for i := range msg.B {
		msg.B[i] = &pb.SizedOk{}
	}
	bz := protoutils.Marshal(msg)
	_, err := protoutils.UnmarshalWithLimit[*pb.OuterNotSized](bz, 1<<20 /* 1MB */)
	require.NoError(t, err)
}

// TestUnmarshalWithLimit_ManyEmptyEntriesRejected verifies the core amplification
// scenario: many empty repeated-message entries are tiny on the wire but each
// cause a Go heap allocation. The limit catches this before proto.Unmarshal runs.
func TestUnmarshalWithLimit_ManyEmptyEntriesRejected(t *testing.T) {
	// 10_000 empty SizedOk entries — small on the wire but each allocates a struct.
	msg := &pb.OuterNotSized{B: make([]*pb.SizedOk, 10_000)}
	for i := range msg.B {
		msg.B[i] = &pb.SizedOk{}
	}
	bz := protoutils.Marshal(msg)
	require.Less(t, len(bz), 1<<20, "wire bytes should be well under 1MB")

	_, err := protoutils.UnmarshalWithLimit[*pb.OuterNotSized](bz, 1<<20 /* 1MB */)
	require.Error(t, err, "10k empty entries should exceed the 1MB allocation estimate")
}

// TestUnmarshalWithLimit_ZeroLimitPanics verifies that limitBytes=0 panics.
// A zero (or negative) limit is a programming error, not a runtime condition.
func TestUnmarshalWithLimit_ZeroLimitPanics(t *testing.T) {
	msg := &pb.OuterNotSized{B: []*pb.SizedOk{{}}}
	bz := protoutils.Marshal(msg)
	require.Panics(t, func() {
		_, _ = protoutils.UnmarshalWithLimit[*pb.OuterNotSized](bz, 0)
	})
	require.Panics(t, func() {
		_, _ = protoutils.UnmarshalWithLimit[*pb.OuterNotSized](bz, -1)
	})
}

// TestUnmarshalWithLimit_ResultIsCorrect verifies that a message passing the
// limit check is correctly unmarshalled.
func TestUnmarshalWithLimit_ResultIsCorrect(t *testing.T) {
	msg := &pb.Msg{StringValue: "hello", RepeatedValue: []string{"a", "b"}}
	bz := protoutils.Marshal(msg)
	got, err := protoutils.UnmarshalWithLimit[*pb.Msg](bz, 1<<20)
	require.NoError(t, err)
	require.Equal(t, "hello", got.StringValue)
	require.Equal(t, []string{"a", "b"}, got.RepeatedValue)
}

// TestUnmarshalWithLimit_LargePayloadRejected verifies that a single large
// bytes field is rejected when it exceeds the limit.
func TestUnmarshalWithLimit_LargePayloadRejected(t *testing.T) {
	msg := &pb.NotSized{LargeField: make([]byte, 2<<20 /* 2MB */)}
	bz := protoutils.Marshal(msg)
	_, err := protoutils.UnmarshalWithLimit[*pb.NotSized](bz, 1<<20 /* 1MB */)
	require.Error(t, err)
}

// TestUnmarshalWithLimit_PackedVarintAmplificationCounted verifies that packed
// repeated uint64 fields with small values are not undercounted. Each value
// encodes as 1 byte on the wire but occupies 8 bytes in the Go slice, giving
// up to 8× amplification that must be accounted for.
func TestUnmarshalWithLimit_PackedVarintAmplificationCounted(t *testing.T) {
	// 200k uint64 values of 0: each encodes as 1 varint byte = ~200KB wire,
	// but the Go slice is 200k×8 = ~1.6MB.
	vals := make([]uint64, 200_000)
	msg := &pb.SizedOk{U64Count: vals[:4]} // SizedOk.U64Count is repeated uint64
	// Build a message with a large packed uint64 field using raw wire bytes
	// since SizedOk caps U64Count at 4. Use field 11 (u64_count) directly.
	var bz []byte
	bz = protowire.AppendTag(bz, 11, protowire.BytesType)
	var packed []byte
	for range 200_000 {
		packed = protowire.AppendVarint(packed, 0)
	}
	bz = protowire.AppendBytes(bz, packed)
	_ = msg

	require.Less(t, len(bz), 1<<20, "wire bytes should be under 1MB")
	_, err := protoutils.UnmarshalWithLimit[*pb.SizedOk](bz, 1<<20 /* 1MB */)
	require.Error(t, err, "200k packed uint64 values should exceed 1MB allocation estimate due to 8x wire-to-Go amplification")
}

// TestUnmarshalWithLimit_UnknownBytesFieldCounted verifies that a large unknown
// bytes field (field number not in the schema) is counted toward the limit.
// proto.Unmarshal stores unknown fields verbatim, allocating exactly len(val)
// bytes, so our estimate must catch this even without knowing the field type.
func TestUnmarshalWithLimit_UnknownBytesFieldCounted(t *testing.T) {
	// Field 999 is unknown to pb.NotSized (which only has field 1).
	var bz []byte
	bz = protowire.AppendTag(bz, 999, protowire.BytesType)
	bz = protowire.AppendBytes(bz, make([]byte, 2<<20 /* 2MB */))

	_, err := protoutils.UnmarshalWithLimit[*pb.NotSized](bz, 1<<20 /* 1MB */)
	require.Error(t, err, "large unknown bytes field must be counted toward the limit")
}

// TestUnmarshalGogoWithLimit_ManyEmptySignaturesRejected verifies that
// UnmarshalGogoWithLimit protects gogoproto-generated Tendermint P2P types.
// A Commit with 100k empty CommitSig entries is tiny on the wire but would
// allocate many structs during Unmarshal.
func TestUnmarshalGogoWithLimit_ManyEmptySignaturesRejected(t *testing.T) {
	// CommitSig has a non-nullable time.Time timestamp that encodes non-zero
	// even when empty, so wire bytes grow faster than a purely empty message.
	// Use 10k entries: small enough to stay comfortably under 1MB on the wire
	// while still causing enough struct allocations to exceed the 1MB limit.
	msg := &tmproto.Commit{Signatures: make([]tmproto.CommitSig, 10_000)}
	bz, err := gogoproto.Marshal(msg)
	require.NoError(t, err)
	require.Less(t, len(bz), 1<<20, "wire bytes should be well under 1MB")

	out := &tmproto.Commit{}
	err = protoutils.UnmarshalGogoWithLimit(bz, out, 1<<20 /* 1MB */)
	require.Error(t, err, "10k CommitSig entries should exceed the 1MB allocation estimate")
}

// TestUnmarshalGogoWithLimit_SingularFieldMerge documents and verifies that
// allocEstimate accumulates all wire occurrences of a singular field.
//
// Protobuf allows a singular field to appear multiple times on the wire;
// gogoproto merges them by appending repeated sub-fields. wireguard Scan
// checks each occurrence independently and passes each one. allocEstimate
// recurses into every occurrence and accumulates the totals, so the budget
// is consumed proportionally to the true decoded size.
func TestUnmarshalGogoWithLimit_SingularFieldMerge(t *testing.T) {
	// Build 500 Commit blobs, each with 50 CommitSig entries. Each occurrence
	// is individually small (~100 bytes wire), but gogoproto would merge them
	// into a single Commit with 25,000 signatures. Total wire size: ~50KB.
	commitPerOccurrence := &tmproto.Commit{Signatures: make([]tmproto.CommitSig, 50)}
	commitBz, err := gogoproto.Marshal(commitPerOccurrence)
	require.NoError(t, err)

	// Build a raw Proposal wire payload with last_commit (field 10) repeated
	// 500 times. Each occurrence is individually small.
	var proposalBz []byte
	for range 500 {
		proposalBz = protowire.AppendTag(proposalBz, 10, protowire.BytesType)
		proposalBz = protowire.AppendBytes(proposalBz, commitBz)
	}
	require.Less(t, len(proposalBz), 1<<20, "wire bytes should be well under 1MB")

	// allocEstimate recurses into all 500 wire occurrences of last_commit,
	// counting 500 × 50 CommitSig structs — exceeding the 1MB limit.
	out := &tmproto.Proposal{}
	err = protoutils.UnmarshalGogoWithLimit(proposalBz, out, 1<<20 /* 1MB */)
	require.Error(t, err, "500 occurrences × 50 signatures should exceed the 1MB allocation estimate")
}

// TestUnmarshalGogoWithLimit_SmallCommitAccepted verifies that a legitimate
// Commit with a handful of signatures passes the limit check.
func TestUnmarshalGogoWithLimit_SmallCommitAccepted(t *testing.T) {
	msg := &tmproto.Commit{Height: 100, Signatures: make([]tmproto.CommitSig, 10)}
	bz, err := gogoproto.Marshal(msg)
	require.NoError(t, err)

	out := &tmproto.Commit{}
	err = protoutils.UnmarshalGogoWithLimit(bz, out, 1<<20 /* 1MB */)
	require.NoError(t, err)
	require.Equal(t, int64(100), out.Height)
}

// TestUnmarshalWithLimit_WireTypeMismatchNoPanic verifies that wire bytes
// presenting a repeated message field with a mismatched scalar wire type do
// not panic. scalarElementSize panics on MessageKind; the mismatch is instead
// counted as unknown-field bytes so the process stays alive.
func TestUnmarshalWithLimit_WireTypeMismatchNoPanic(t *testing.T) {
	// Field 2 of OuterNotSized is repeated SizedOk (MessageKind), but we encode
	// it as a varint — a wire type mismatch. Each occurrence is stored in the
	// unknown-fields blob (~2 bytes).
	var one []byte
	one = protowire.AppendTag(one, 2, protowire.VarintType)
	one = protowire.AppendVarint(one, 42)

	// A single mismatch: well within limit, must not panic.
	_, err := protoutils.UnmarshalWithLimit[*pb.OuterNotSized](one, 1<<20)
	require.NoError(t, err)

	// Many mismatches: each contributes ~2 bytes to the estimate, so enough
	// occurrences must exceed the 1 byte limit, proving they are counted.
	var many []byte
	for range 1_000_000 {
		many = protowire.AppendTag(many, 2, protowire.VarintType)
		many = protowire.AppendVarint(many, 42)
	}
	_, err = protoutils.UnmarshalWithLimit[*pb.OuterNotSized](many, 1)
	require.Error(t, err, "1M mismatched-type occurrences must exceed a 1-byte limit")
}

// TestUnmarshalWithLimit_LargeMapRejected verifies that a message with many map
// entries is rejected when the entries' total allocation would exceed the limit.
// Map fields encode as repeated message entries; Go allocates runtime map
// overhead per entry beyond the key+value content.
func TestUnmarshalWithLimit_LargeMapRejected(t *testing.T) {
	// 50k entries in a map<string,string>: small values but each entry adds
	// string headers + key + value + map runtime overhead.
	msg := &pb.Msg{MapValue: make(map[string]string, 50_000)}
	for i := range 50_000 {
		k := fmt.Sprintf("key%d", i)
		msg.MapValue[k] = "v"
	}
	bz := protoutils.Marshal(msg)
	_, err := protoutils.UnmarshalWithLimit[*pb.Msg](bz, 1<<20 /* 1MB */)
	require.Error(t, err, "50k map entries should exceed the 1MB allocation estimate")
}

// TestUnmarshalWithLimit_SmallMapAccepted verifies that a small map passes.
func TestUnmarshalWithLimit_SmallMapAccepted(t *testing.T) {
	msg := &pb.Msg{MapValue: map[string]string{"a": "b", "c": "d"}}
	bz := protoutils.Marshal(msg)
	_, err := protoutils.UnmarshalWithLimit[*pb.Msg](bz, 1<<20 /* 1MB */)
	require.NoError(t, err)
}

// TestUnmarshalWithLimit_TruncatedInputReturnsError verifies that wire bytes
// cut off mid-field return an error rather than panicking or silently
// accepting partial data. Truncation surfaces as protowire.ParseError
// ("unexpected end of data"), the same path as corrupt wire bytes.
//
// Note: a prefix that ends exactly on a field boundary is a valid shorter
// proto message (proto has no end-of-message marker), so we construct inputs
// that are definitely cut mid-field.
func TestUnmarshalWithLimit_TruncatedInputReturnsError(t *testing.T) {
	// Case 1: tag present but bytes-field length prefix missing.
	// A lone tag byte for a BytesType field with no following length varint.
	var tagOnly []byte
	tagOnly = protowire.AppendTag(tagOnly, 1, protowire.BytesType)
	_, err := protoutils.UnmarshalWithLimit[*pb.NotSized](tagOnly, 1<<20)
	require.Error(t, err, "tag with no value should return an error")

	// Case 2: bytes-field length prefix present but payload truncated.
	// Claim 100 bytes follow, but provide only 10.
	var truncBytes []byte
	truncBytes = protowire.AppendTag(truncBytes, 1, protowire.BytesType)
	truncBytes = protowire.AppendVarint(truncBytes, 100) // length = 100
	truncBytes = append(truncBytes, make([]byte, 10)...) // only 10 bytes
	_, err = protoutils.UnmarshalWithLimit[*pb.NotSized](truncBytes, 1<<20)
	require.Error(t, err, "truncated bytes payload should return an error")

	// Case 3: varint field tag present but varint value truncated mid-byte.
	// A varint with the MSB set signals continuation; cut before the last byte.
	var truncVarint []byte
	truncVarint = protowire.AppendTag(truncVarint, 1, protowire.VarintType)
	truncVarint = append(truncVarint, 0x80) // first varint byte with continuation bit set
	_, err = protoutils.UnmarshalWithLimit[*pb.Msg](truncVarint, 1<<20)
	require.Error(t, err, "truncated mid-varint should return an error")
}

// TestUnmarshalWithLimit_UnpackedRepeatedScalarSliceHeaderCounted verifies that
// non-packed repeated scalar fields (Fixed64Type wire encoding) include the
// slice header in the allocation estimate. Each occurrence contributes
// sliceHeaderSize + elementSize, not just elementSize.
func TestUnmarshalWithLimit_UnpackedRepeatedScalarSliceHeaderCounted(t *testing.T) {
	// SizedOk.f64_count is repeated fixed64 (field 14). fixed64 always uses
	// Fixed64Type wire encoding — never packed. Each occurrence costs 8 bytes
	// element + 24 bytes slice header = 32 bytes in the estimate.
	// 100k occurrences × 32 = ~3.2MB, well over the 1MB limit.
	// Wire size: 100k × (1 tag + 8 value) = ~900KB, under 1MB.
	var bz []byte
	for range 100_000 {
		bz = protowire.AppendTag(bz, 14, protowire.Fixed64Type)
		bz = protowire.AppendFixed64(bz, 0)
	}
	require.Less(t, len(bz), 1<<20, "wire bytes should be under 1MB")
	_, err := protoutils.UnmarshalWithLimit[*pb.SizedOk](bz, 1<<20)
	require.Error(t, err, "100k unpacked fixed64 elements should exceed 1MB due to per-occurrence slice header cost")
}

// TestUnmarshalWithLimit_SmallUnknownFieldsAccepted verifies that small unknown
// scalar fields (varint, fixed32, fixed64) are accepted within a generous limit.
func TestUnmarshalWithLimit_SmallUnknownFieldsAccepted(t *testing.T) {
	var bz []byte
	for i := protowire.Number(100); i < 200; i++ {
		bz = protowire.AppendTag(bz, i, protowire.VarintType)
		bz = protowire.AppendVarint(bz, 42)
		bz = protowire.AppendTag(bz, i+1000, protowire.Fixed32Type)
		bz = protowire.AppendFixed32(bz, 0xdeadbeef)
		bz = protowire.AppendTag(bz, i+2000, protowire.Fixed64Type)
		bz = protowire.AppendFixed64(bz, 0xdeadbeefcafe)
	}

	_, err := protoutils.UnmarshalWithLimit[*pb.NotSized](bz, 1<<20 /* 1MB */)
	require.NoError(t, err, "small unknown scalar fields should be well within a 1MB limit")
}
