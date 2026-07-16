package protoutils_test

import (
	"math"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type maxSizer interface {
	MaxSize() int
}

func TestMaxSize(t *testing.T) {
	t.Parallel()

	sized := &pb.Sized{A: math.MaxUint64}
	sizedOk := &pb.SizedOk{
		U64:                 math.MaxUint64,
		I64:                 -1,
		S64:                 math.MinInt64,
		F64:                 math.MaxUint64,
		F:                   math.MaxFloat32,
		D:                   math.MaxFloat64,
		S:                   sized,
		BSize:               []byte(strings.Repeat("x", 5)),
		SSize:               strings.Repeat("x", 5),
		NSize:               &pb.NotSized{LargeField: []byte(strings.Repeat("x", 18))},
		U64Count:            []uint64{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64},
		I64Count:            []int64{-1, -1, -1, -1},
		S64Count:            []int64{math.MinInt64, math.MinInt64, math.MinInt64, math.MinInt64},
		F64Count:            []uint64{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64},
		FCount:              []float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32, math.MaxFloat32},
		DCount:              []float64{math.MaxFloat64, math.MaxFloat64, math.MaxFloat64, math.MaxFloat64},
		SCount:              []*pb.Sized{sized, sized, sized, sized},
		BCountSize:          [][]byte{[]byte("xxxxx"), []byte("xxxxx"), []byte("xxxxx")},
		SCountSize:          []string{"xxxxx", "xxxxx", "xxxxx"},
		NCountSize:          []*pb.NotSized{{LargeField: []byte("xxx")}, {LargeField: []byte("xxx")}, {LargeField: []byte("xxx")}},
		BCountTotalSize:     [][]byte{[]byte("xxx"), []byte("xxx"), []byte("xxxx")},
		SCountTotalSize:     []string{"xxx", "xxx", "xxxx"},
		NCountTotalSize:     []*pb.NotSized{{LargeField: []byte("x")}, {LargeField: []byte("x")}, {LargeField: []byte("xx")}},
		BCountSizeTotalSize: [][]byte{[]byte("xxx"), []byte("xx"), []byte("xxxxx")},
		SCountSizeTotalSize: []string{"xxx", "xx", "xxxxx"},
		NCountSizeTotalSize: []*pb.NotSized{{LargeField: []byte("x")}, {LargeField: []byte("xx")}, {LargeField: []byte("x")}},
		O:                   &pb.SizedOk_ONSize{ONSize: &pb.NotSized{LargeField: []byte(strings.Repeat("x", 18))}},
	}
	outerSized := &pb.OuterSized{
		A: sizedOk,
		B: []*pb.SizedOk{sizedOk, sizedOk, sizedOk, sizedOk, sizedOk, sizedOk, sizedOk},
	}
	packedFalseSized := &pb.PackedFalseSized{Flags: []bool{true}}

	require.GreaterOrEqual(t, sized.MaxSize(), protoutils.Size(sized))
	require.GreaterOrEqual(t, sizedOk.MaxSize(), protoutils.Size(sizedOk))
	require.GreaterOrEqual(t, outerSized.MaxSize(), protoutils.Size(outerSized))
	require.Equal(t, 3, packedFalseSized.MaxSize())
}

func TestMaxSizeOnlyOnSizedMessages(t *testing.T) {
	t.Parallel()

	_, sized := any(&pb.Sized{}).(maxSizer)
	_, sizedOk := any(&pb.SizedOk{}).(maxSizer)
	_, outerSized := any(&pb.OuterSized{}).(maxSizer)
	_, packedFalseSized := any(&pb.PackedFalseSized{}).(maxSizer)
	_, notSized := any(&pb.NotSized{}).(maxSizer)
	_, outerNotSized := any(&pb.OuterNotSized{}).(maxSizer)

	require.True(t, sized)
	require.True(t, sizedOk)
	require.True(t, outerSized)
	require.True(t, packedFalseSized)
	require.False(t, notSized)
	require.False(t, outerNotSized)
}
