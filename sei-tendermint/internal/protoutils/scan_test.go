package protoutils_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestScan(t *testing.T) {
	t.Parallel()

	rng := utils.TestRng()
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	randomString := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = letters[rng.Intn(len(letters))]
		}
		return string(b)
	}
	randomBytes := func(n int) []byte {
		return utils.GenBytes(rng, n)
	}
	randomSized := func() *pb.Sized {
		return &pb.Sized{A: rng.Uint64()}
	}
	randomNotSized := func(payloadSize int) *pb.NotSized {
		return &pb.NotSized{LargeField: randomBytes(payloadSize)}
	}
	randomSizedOk := func() *pb.SizedOk {
		return &pb.SizedOk{
			U64:                 rng.Uint64(),
			I64:                 int64(rng.Uint64()),
			S64:                 int64(rng.Uint64()),
			F64:                 rng.Uint64(),
			F:                   float32(rng.Intn(1<<16)) / 37,
			D:                   float64(rng.Uint64()) / 37,
			S:                   randomSized(),
			BSize:               randomBytes(5),
			SSize:               randomString(5),
			NSize:               randomNotSized(18),
			U64Count:            []uint64{rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()},
			I64Count:            []int64{int64(rng.Uint64()), int64(rng.Uint64()), int64(rng.Uint64()), int64(rng.Uint64())},
			S64Count:            []int64{int64(rng.Uint64()), int64(rng.Uint64()), int64(rng.Uint64()), int64(rng.Uint64())},
			F64Count:            []uint64{rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()},
			FCount:              []float32{1.25, 2.5, 3.75, 4.125},
			DCount:              []float64{1.5, 2.25, 3.125, 4.0625},
			SCount:              []*pb.Sized{randomSized(), randomSized(), randomSized(), randomSized()},
			BCountSize:          [][]byte{randomBytes(5), randomBytes(5), randomBytes(5)},
			SCountSize:          []string{randomString(5), randomString(5), randomString(5)},
			NCountSize:          []*pb.NotSized{randomNotSized(3), randomNotSized(3), randomNotSized(3)},
			BCountTotalSize:     [][]byte{randomBytes(3), randomBytes(3), randomBytes(4)},
			SCountTotalSize:     []string{randomString(3), randomString(3), randomString(4)},
			NCountTotalSize:     []*pb.NotSized{randomNotSized(1), randomNotSized(1), randomNotSized(2)},
			BCountSizeTotalSize: [][]byte{randomBytes(3), randomBytes(2), randomBytes(5)},
			SCountSizeTotalSize: []string{randomString(3), randomString(2), randomString(5)},
			NCountSizeTotalSize: []*pb.NotSized{randomNotSized(1), randomNotSized(2), randomNotSized(1)},
			O:                   &pb.SizedOk_ONSize{ONSize: randomNotSized(18)},
		}
	}

	baseSizedOk := randomSizedOk()
	baseOuterSized := &pb.OuterSized{
		A: protoutils.Clone(baseSizedOk),
		B: []*pb.SizedOk{
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
		},
	}
	baseOuterNotSized := &pb.OuterNotSized{
		A: protoutils.Clone(baseSizedOk),
		B: []*pb.SizedOk{
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
			randomSizedOk(),
		},
		C: randomNotSized(256),
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

	type testCase struct {
		name    string
		wantErr bool
		run     func() error
	}

	cases := []testCase{
		{name: "SizedOk valid", run: func() error { return scanSizedOk(protoutils.Clone(baseSizedOk)) }},
		{name: "OuterSized valid", run: func() error { return scanOuterSized(protoutils.Clone(baseOuterSized)) }},
		{name: "OuterNotSized valid", run: func() error { return scanOuterNotSized(protoutils.Clone(baseOuterNotSized)) }},
		{name: "SizedOk field 8 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BSize = randomBytes(6)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 9 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SSize = randomString(6)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 10 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NSize = randomNotSized(19)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 11 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.U64Count = append(msg.U64Count, rng.Uint64())
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 12 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.I64Count = append(msg.I64Count, int64(rng.Uint64()))
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 13 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.S64Count = append(msg.S64Count, int64(rng.Uint64()))
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 14 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.F64Count = append(msg.F64Count, rng.Uint64())
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 15 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.FCount = append(msg.FCount, 5.25)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 16 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.DCount = append(msg.DCount, 5.125)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 17 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCount = append(msg.SCount, randomSized())
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 18 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountSize = [][]byte{randomBytes(1), randomBytes(1), randomBytes(1), randomBytes(1)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 18 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountSize[0] = randomBytes(6)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 19 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountSize = []string{randomString(1), randomString(1), randomString(1), randomString(1)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 19 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountSize[0] = randomString(6)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 20 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountSize = []*pb.NotSized{{}, {}, {}, {}}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 20 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountSize[0] = randomNotSized(4)
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 21 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountTotalSize = [][]byte{randomBytes(3), randomBytes(3), randomBytes(2), randomBytes(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 21 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountTotalSize = [][]byte{randomBytes(3), randomBytes(3), randomBytes(5)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 22 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountTotalSize = []string{randomString(3), randomString(3), randomString(2), randomString(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 22 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountTotalSize = []string{randomString(3), randomString(3), randomString(5)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 23 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountTotalSize = []*pb.NotSized{{}, {}, {}, {}}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 23 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountTotalSize = []*pb.NotSized{randomNotSized(1), randomNotSized(1), randomNotSized(3)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 24 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountSizeTotalSize = [][]byte{randomBytes(3), randomBytes(3), randomBytes(2), randomBytes(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 24 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountSizeTotalSize = [][]byte{randomBytes(6), randomBytes(2), randomBytes(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 24 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.BCountSizeTotalSize = [][]byte{randomBytes(5), randomBytes(5), randomBytes(1)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 25 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountSizeTotalSize = []string{randomString(3), randomString(3), randomString(2), randomString(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 25 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountSizeTotalSize = []string{randomString(6), randomString(2), randomString(2)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 25 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.SCountSizeTotalSize = []string{randomString(5), randomString(5), randomString(1)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 26 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountSizeTotalSize = []*pb.NotSized{{}, {}, {}, {}}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 26 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountSizeTotalSize = []*pb.NotSized{randomNotSized(4), {}, {}}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 26 max_total_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.NCountSizeTotalSize = []*pb.NotSized{randomNotSized(3), randomNotSized(3), randomNotSized(1)}
			return scanSizedOk(msg)
		}},
		{name: "SizedOk field 28 max_size", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseSizedOk)
			msg.O = &pb.SizedOk_ONSize{ONSize: randomNotSized(19)}
			return scanSizedOk(msg)
		}},
		{name: "OuterSized field 1 nested", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterSized)
			msg.A.BSize = randomBytes(6)
			return scanOuterSized(msg)
		}},
		{name: "OuterSized field 2 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterSized)
			msg.B = append(msg.B, randomSizedOk())
			return scanOuterSized(msg)
		}},
		{name: "OuterSized field 2 nested", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterSized)
			msg.B[0].BSize = randomBytes(6)
			return scanOuterSized(msg)
		}},
		{name: "OuterNotSized field 1 nested", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterNotSized)
			msg.A.BSize = randomBytes(6)
			return scanOuterNotSized(msg)
		}},
		{name: "OuterNotSized field 2 max_count", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterNotSized)
			msg.B = append(msg.B, randomSizedOk())
			return scanOuterNotSized(msg)
		}},
		{name: "OuterNotSized field 2 nested", wantErr: true, run: func() error {
			msg := protoutils.Clone(baseOuterNotSized)
			msg.B[0].BSize = randomBytes(6)
			return scanOuterNotSized(msg)
		}},
	}

	for _, tc := range cases {
		err := tc.run()
		if tc.wantErr {
			require.Error(t, err, tc.name)
		} else {
			require.NoError(t, err, tc.name)
		}
	}
}
