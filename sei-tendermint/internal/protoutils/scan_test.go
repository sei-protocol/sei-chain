package protoutils_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// Test generating message(s) up to the limits.
// Then checking if Scan succeeds.
// Then exceeding all limits one by one and checking if Scan fails.
// We are running Scan on the message in question, and on an outer sized/unsized message which embeds our message.
func TestScan(t *testing.T) {
	t.Parallel()
	rng := utils.TestRng()

	genBytes := func(rng utils.Rng, sizes ...int) [][]byte {
		items := make([][]byte, len(sizes))
		for i, size := range sizes {
			items[i] = utils.GenBytes(rng, size)
		}
		return items
	}
	genStrings := func(rng utils.Rng, sizes ...int) []string {
		items := make([]string, len(sizes))
		for i, size := range sizes {
			items[i] = utils.GenString(rng, size)
		}
		return items
	}
	genSized := func(rng utils.Rng) *pb.Sized { return &pb.Sized{A: rng.Uint64()} }
	genNotSized := func(rng utils.Rng, payloadSize int) *pb.NotSized {
		return &pb.NotSized{LargeField: utils.GenBytes(rng, payloadSize)}
	}
	genNotSizedSlice := func(rng utils.Rng, sizes ...int) []*pb.NotSized {
		a := make([]*pb.NotSized, len(sizes))
		for i := range a {
			a[i] = genNotSized(rng, sizes[i])
		}
		return a
	}
	genSizedOk := func(rng utils.Rng) *pb.SizedOk {
		return &pb.SizedOk{
			U64:                 rng.Uint64(),
			I64:                 int64(rng.Uint64()),
			S64:                 int64(rng.Uint64()),
			F64:                 rng.Uint64(),
			F:                   float32(rng.Intn(1<<16)) / 37,
			D:                   float64(rng.Uint64()) / 37,
			S:                   genSized(rng),
			BSize:               utils.GenBytes(rng, 5),
			SSize:               utils.GenString(rng, 5),
			NSize:               genNotSized(rng, 18),
			U64Count:            utils.GenSliceN(rng, 4, func(rng utils.Rng) uint64 { return rng.Uint64() }),
			I64Count:            utils.GenSliceN(rng, 4, func(rng utils.Rng) int64 { return int64(rng.Uint64()) }),
			S64Count:            utils.GenSliceN(rng, 4, func(rng utils.Rng) int64 { return int64(rng.Uint64()) }),
			F64Count:            utils.GenSliceN(rng, 4, func(rng utils.Rng) uint64 { return rng.Uint64() }),
			FCount:              []float32{1.25, 2.5, 3.75, 4.125},
			DCount:              []float64{1.5, 2.25, 3.125, 4.0625},
			SCount:              utils.GenSliceN(rng, 4, genSized),
			BoolCountUnpacked:   []bool{true},
			BCountSize:          genBytes(rng, 5, 5, 5),
			SCountSize:          genStrings(rng, 5, 5, 5),
			NCountSize:          genNotSizedSlice(rng, 3, 3, 3),
			BCountTotalSize:     genBytes(rng, 3, 3, 4),
			SCountTotalSize:     genStrings(rng, 3, 3, 4),
			NCountTotalSize:     genNotSizedSlice(rng, 1, 1, 2),
			BCountSizeTotalSize: genBytes(rng, 3, 2, 5),
			SCountSizeTotalSize: genStrings(rng, 3, 2, 5),
			NCountSizeTotalSize: genNotSizedSlice(rng, 1, 2, 1),
			O:                   &pb.SizedOk_ONSize{ONSize: genNotSized(rng, 18)},
		}
	}

	baseSizedOk := genSizedOk(rng)
	baseOuterSized := &pb.OuterSized{
		A: protoutils.Clone(baseSizedOk),
		B: utils.GenSliceN(rng, 7, func(rng utils.Rng) *pb.SizedOk { return genSizedOk(rng) }),
	}
	baseOuterNotSized := &pb.OuterNotSized{
		A: protoutils.Clone(baseSizedOk),
		B: utils.GenSliceN(rng, 7, func(rng utils.Rng) *pb.SizedOk { return genSizedOk(rng) }),
		C: genNotSized(rng, 256),
	}

	scanSizedOk := func(msg *pb.SizedOk) error {
		return protoutils.Scan[*pb.SizedOk](protoutils.Marshal(msg))
	}
	scanOuterSized := func(msg *pb.OuterSized) error {
		return protoutils.Scan[*pb.OuterSized](protoutils.Marshal(msg))
	}
	scanOuterNotSized := func(msg *pb.OuterNotSized) error {
		return protoutils.Scan[*pb.OuterNotSized](protoutils.Marshal(msg))
	}

	cloneSizedOkAndApply := func(msg *pb.SizedOk, mutators []func(*pb.SizedOk)) []*pb.SizedOk {
		var variants []*pb.SizedOk
		for _, mutate := range mutators {
			clone := protoutils.Clone(msg)
			mutate(clone)
			variants = append(variants, clone)
		}
		return variants
	}

	sizedOkMalformedVariants := func(msg *pb.SizedOk) []*pb.SizedOk {
		return cloneSizedOkAndApply(msg, []func(*pb.SizedOk){
			func(msg *pb.SizedOk) { msg.BSize = utils.GenBytes(rng, 6) },
			func(msg *pb.SizedOk) { msg.SSize = utils.GenString(rng, 6) },
			func(msg *pb.SizedOk) { msg.NSize = genNotSized(rng, 19) },
			func(msg *pb.SizedOk) { msg.U64Count = append(msg.U64Count, rng.Uint64()) },
			func(msg *pb.SizedOk) { msg.I64Count = append(msg.I64Count, int64(rng.Uint64())) },
			func(msg *pb.SizedOk) { msg.S64Count = append(msg.S64Count, int64(rng.Uint64())) },
			func(msg *pb.SizedOk) { msg.F64Count = append(msg.F64Count, rng.Uint64()) },
			func(msg *pb.SizedOk) { msg.FCount = append(msg.FCount, 5.25) },
			func(msg *pb.SizedOk) { msg.DCount = append(msg.DCount, 5.125) },
			func(msg *pb.SizedOk) { msg.SCount = append(msg.SCount, genSized(rng)) },
			func(msg *pb.SizedOk) { msg.BoolCountUnpacked = append(msg.BoolCountUnpacked, false) },
			func(msg *pb.SizedOk) { msg.BCountSize = genBytes(rng, 1, 1, 1, 1) },
			func(msg *pb.SizedOk) { msg.BCountSize[0] = utils.GenBytes(rng, 6) },
			func(msg *pb.SizedOk) { msg.SCountSize = genStrings(rng, 1, 1, 1, 1) },
			func(msg *pb.SizedOk) { msg.SCountSize[0] = utils.GenString(rng, 6) },
			func(msg *pb.SizedOk) { msg.NCountSize = genNotSizedSlice(rng, 0, 0, 0, 0) },
			func(msg *pb.SizedOk) { msg.NCountSize[0] = genNotSized(rng, 4) },
			func(msg *pb.SizedOk) { msg.BCountTotalSize = genBytes(rng, 3, 3, 2, 2) },
			func(msg *pb.SizedOk) { msg.BCountTotalSize = genBytes(rng, 3, 3, 5) },
			func(msg *pb.SizedOk) { msg.SCountTotalSize = genStrings(rng, 3, 3, 2, 2) },
			func(msg *pb.SizedOk) { msg.SCountTotalSize = genStrings(rng, 3, 3, 5) },
			func(msg *pb.SizedOk) { msg.NCountTotalSize = genNotSizedSlice(rng, 0, 0, 0, 0) },
			func(msg *pb.SizedOk) { msg.NCountTotalSize = genNotSizedSlice(rng, 1, 1, 3) },
			func(msg *pb.SizedOk) { msg.BCountSizeTotalSize = genBytes(rng, 3, 3, 2, 2) },
			func(msg *pb.SizedOk) { msg.BCountSizeTotalSize = genBytes(rng, 6, 2, 2) },
			func(msg *pb.SizedOk) { msg.BCountSizeTotalSize = genBytes(rng, 5, 5, 1) },
			func(msg *pb.SizedOk) { msg.SCountSizeTotalSize = genStrings(rng, 3, 3, 2, 2) },
			func(msg *pb.SizedOk) { msg.SCountSizeTotalSize = genStrings(rng, 6, 2, 2) },
			func(msg *pb.SizedOk) { msg.SCountSizeTotalSize = genStrings(rng, 5, 5, 1) },
			func(msg *pb.SizedOk) { msg.NCountSizeTotalSize = genNotSizedSlice(rng, 0, 0, 0, 0) },
			func(msg *pb.SizedOk) { msg.NCountSizeTotalSize = genNotSizedSlice(rng, 4, 0, 0) },
			func(msg *pb.SizedOk) { msg.NCountSizeTotalSize = genNotSizedSlice(rng, 3, 3, 1) },
			func(msg *pb.SizedOk) { msg.O = &pb.SizedOk_ONSize{ONSize: genNotSized(rng, 19)} },
		})
	}

	outerSizedMalformedVariants := func(msg *pb.OuterSized) []*pb.OuterSized {
		var variants []*pb.OuterSized
		for _, malformed := range sizedOkMalformedVariants(msg.A) {
			clone := protoutils.Clone(msg)
			clone.A = malformed
			variants = append(variants, clone)
		}
		for i := range msg.B {
			for _, malformed := range sizedOkMalformedVariants(msg.B[i]) {
				clone := protoutils.Clone(msg)
				clone.B[i] = malformed
				variants = append(variants, clone)
			}
		}
		clone := protoutils.Clone(msg)
		clone.B = append(clone.B, genSizedOk(rng))
		variants = append(variants, clone)
		return variants
	}

	outerNotSizedMalformedVariants := func(msg *pb.OuterNotSized) []*pb.OuterNotSized {
		var variants []*pb.OuterNotSized
		for _, malformed := range sizedOkMalformedVariants(msg.A) {
			clone := protoutils.Clone(msg)
			clone.A = malformed
			variants = append(variants, clone)
		}
		for i := range msg.B {
			for _, malformed := range sizedOkMalformedVariants(msg.B[i]) {
				clone := protoutils.Clone(msg)
				clone.B[i] = malformed
				variants = append(variants, clone)
			}
		}
		clone := protoutils.Clone(msg)
		clone.B = append(clone.B, genSizedOk(rng))
		variants = append(variants, clone)
		return variants
	}

	require.NoError(t, scanSizedOk(protoutils.Clone(baseSizedOk)))
	require.NoError(t, scanOuterSized(protoutils.Clone(baseOuterSized)))
	require.NoError(t, scanOuterNotSized(protoutils.Clone(baseOuterNotSized)))

	for _, malformed := range sizedOkMalformedVariants(baseSizedOk) {
		require.Error(t, scanSizedOk(malformed))
	}
	for _, malformed := range outerSizedMalformedVariants(baseOuterSized) {
		require.Error(t, scanOuterSized(malformed))
	}
	for _, malformed := range outerNotSizedMalformedVariants(baseOuterNotSized) {
		require.Error(t, scanOuterNotSized(malformed))
	}
}

func TestScan_PackedInputAcceptedForPackedFalseField(t *testing.T) {
	t.Parallel()

	var packedAtLimit []byte
	packedAtLimit = protowire.AppendTag(packedAtLimit, 1, protowire.BytesType)
	packedAtLimit = protowire.AppendBytes(packedAtLimit, protowire.AppendVarint(nil, 1))
	require.NoError(t, protoutils.Scan[*pb.PackedFalseSized](packedAtLimit))

	var packedOverLimit []byte
	packedOverLimit = protowire.AppendTag(packedOverLimit, 1, protowire.BytesType)
	packedOverLimit = protowire.AppendBytes(packedOverLimit, append(protowire.AppendVarint(nil, 1), protowire.AppendVarint(nil, 0)...))
	require.Error(t, protoutils.Scan[*pb.PackedFalseSized](packedOverLimit))
}

func TestScan_RejectsDuplicateSingularFieldsInSizedMessages(t *testing.T) {
	t.Parallel()

	var sizedDup []byte
	sizedDup = protowire.AppendTag(sizedDup, 1, protowire.VarintType)
	sizedDup = protowire.AppendVarint(sizedDup, 1)
	sizedDup = protowire.AppendTag(sizedDup, 1, protowire.VarintType)
	sizedDup = protowire.AppendVarint(sizedDup, 2)
	require.Error(t, protoutils.Scan[*pb.Sized](sizedDup))

	var outerSizedDup []byte
	inner := protowire.AppendTag(nil, 1, protowire.VarintType)
	inner = protowire.AppendVarint(inner, 1)
	outerSizedDup = protowire.AppendTag(outerSizedDup, 1, protowire.BytesType)
	outerSizedDup = protowire.AppendBytes(outerSizedDup, inner)
	outerSizedDup = protowire.AppendTag(outerSizedDup, 1, protowire.BytesType)
	outerSizedDup = protowire.AppendBytes(outerSizedDup, inner)
	require.Error(t, protoutils.Scan[*pb.OuterSized](outerSizedDup))
}

func TestScan_RejectsDuplicateSingularFieldsInUnsizedMessages(t *testing.T) {
	t.Parallel()

	payload := protowire.AppendTag(nil, 1, protowire.BytesType)
	payload = protowire.AppendBytes(payload, []byte{1})

	var outerNotSizedDup []byte
	outerNotSizedDup = protowire.AppendTag(outerNotSizedDup, 3, protowire.BytesType)
	outerNotSizedDup = protowire.AppendBytes(outerNotSizedDup, payload)
	outerNotSizedDup = protowire.AppendTag(outerNotSizedDup, 3, protowire.BytesType)
	outerNotSizedDup = protowire.AppendBytes(outerNotSizedDup, payload)
	require.Error(t, protoutils.Scan[*pb.OuterNotSized](outerNotSizedDup))
}

func TestScan_AllowsMultipleMapEntriesInUnsizedMessages(t *testing.T) {
	t.Parallel()

	msg := &pb.Msg{
		MapValue: map[string]string{
			"a": "1",
			"b": "2",
		},
		MapMessageValue: map[string]*pb.Child{
			"x": {Value: "left"},
			"y": {Value: "right"},
		},
	}

	require.NoError(t, protoutils.Scan[*pb.Msg](protoutils.Marshal(msg)))
}

func TestScan_ScansMapMessageValues(t *testing.T) {
	t.Parallel()

	valid := &pb.Msg{
		MapMessageValue: map[string]*pb.Child{
			"x": {Value: "left"},
		},
	}
	require.NoError(t, protoutils.Scan[*pb.Msg](protoutils.Marshal(valid)))

	child := protowire.AppendTag(nil, 1, protowire.BytesType)
	child = protowire.AppendString(child, "first")
	child = protowire.AppendTag(child, 1, protowire.BytesType)
	child = protowire.AppendString(child, "second")

	entry := protowire.AppendTag(nil, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "k")
	entry = protowire.AppendTag(entry, 2, protowire.BytesType)
	entry = protowire.AppendBytes(entry, child)

	msg := protowire.AppendTag(nil, 5, protowire.BytesType)
	msg = protowire.AppendBytes(msg, entry)

	require.Error(t, protoutils.Scan[*pb.Msg](msg))
}

func TestScan_RejectsMalformedNestedMapMessageValue(t *testing.T) {
	t.Parallel()

	child := protowire.AppendTag(nil, 1, protowire.BytesType)
	child = protowire.AppendString(child, "first")
	child = protowire.AppendTag(child, 1, protowire.BytesType)
	child = protowire.AppendString(child, "second")

	entry := protowire.AppendTag(nil, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "k")
	entry = protowire.AppendTag(entry, 2, protowire.BytesType)
	entry = protowire.AppendBytes(entry, child)

	msg := protowire.AppendTag(nil, 5, protowire.BytesType)
	msg = protowire.AppendBytes(msg, entry)

	require.Error(t, protoutils.Scan[*pb.Msg](msg))
}

func TestScan_RejectsDuplicateMapEntryKey(t *testing.T) {
	t.Parallel()

	entry := protowire.AppendTag(nil, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "left")
	entry = protowire.AppendTag(entry, 1, protowire.BytesType)
	entry = protowire.AppendString(entry, "right")

	msg := protowire.AppendTag(nil, 3, protowire.BytesType)
	msg = protowire.AppendBytes(msg, entry)

	require.Error(t, protoutils.Scan[*pb.Msg](msg))
}
