package evmonly

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func gwei(n uint64) uint256.Int {
	return *uint256.NewInt(n * params.GWei)
}

const (
	second      = uint64(1_000_000_000)
	millisecond = second / 1_000
)

func TestNextBaseFee(t *testing.T) {
	// The Giga 1559 worked example: a 500M gas target over one-second blocks and D = 144, with a
	// 100 gwei floor.
	example := BaseFeeParams{Floor: gwei(100), TargetGasPerSecond: 500_000_000, Denominator: 144}
	// The launch rate, 2 giga-gas per second, targets 500M gas over a 250ms block.
	launch := BaseFeeParams{Floor: gwei(100), TargetGasPerSecond: 2_000_000_000, Denominator: 144}
	noFloor := BaseFeeParams{TargetGasPerSecond: 500_000_000, Denominator: 144}
	var maxFee uint256.Int
	maxFee.SetAllOne()

	for _, tc := range []struct {
		name      string
		params    BaseFeeParams
		scheduled uint256.Int
		gasUsed   uint64
		elapsed   uint64
		want      uint256.Int
	}{
		{
			name:   "above target raises the fee by the excess over D",
			params: example, scheduled: gwei(120), gasUsed: 710_000_000, elapsed: second,
			want: *uint256.NewInt(120_350_000_000),
		},
		{
			name:   "below target lowers the fee by the shortfall over D",
			params: example, scheduled: gwei(120), gasUsed: 290_000_000, elapsed: second,
			want: *uint256.NewInt(119_650_000_000),
		},
		{
			name:   "the target is the rate over the block's duration",
			params: launch, scheduled: gwei(120), gasUsed: 710_000_000, elapsed: 250 * millisecond,
			want: *uint256.NewInt(120_350_000_000),
		},
		{
			name:   "the same gas over a longer block is below target",
			params: launch, scheduled: gwei(120), gasUsed: 710_000_000, elapsed: 500 * millisecond,
			want: *uint256.NewInt(119_758_333_334),
		},
		{
			name:   "below target never goes under the floor",
			params: example, scheduled: gwei(100), gasUsed: 290_000_000, elapsed: second,
			want: gwei(100),
		},
		{
			name:   "at target leaves the fee unchanged",
			params: example, scheduled: gwei(120), gasUsed: 500_000_000, elapsed: second,
			want: gwei(120),
		},
		{
			name:   "twice the target raises the fee by 1/D",
			params: example, scheduled: gwei(144), gasUsed: 1_000_000_000, elapsed: second,
			want: gwei(145),
		},
		{
			name:   "sixty times the target raises the fee by 59/D",
			params: example, scheduled: gwei(144), gasUsed: 30_000_000_000, elapsed: second,
			want: gwei(203),
		},
		{
			name:   "a block a microsecond after its parent has a target of 2,000 gas",
			params: launch, scheduled: gwei(144), gasUsed: 2_000_000_000, elapsed: 1_000,
			want: gwei(1_000_143),
		},
		{
			name:   "a block that takes no time leaves the fee unchanged",
			params: launch, scheduled: gwei(144), gasUsed: 21_000, elapsed: 0,
			want: gwei(144),
		},
		{
			name:   "a block that takes no time still raises the fee to the floor",
			params: launch, scheduled: gwei(50), gasUsed: 21_000, elapsed: 0,
			want: gwei(100),
		},
		{
			name:   "an empty block lowers the fee by 1/D",
			params: example, scheduled: gwei(144), gasUsed: 0, elapsed: second,
			want: gwei(143),
		},
		{
			name:   "a block decades after its parent lowers the fee by less than 1/D",
			params: launch, scheduled: gwei(144), gasUsed: 2_000_000_000, elapsed: 1 << 62,
			want: *uint256.NewInt(143_000_000_001),
		},
		{
			name:   "an increase rounding to zero is one wei",
			params: noFloor, scheduled: *uint256.NewInt(1_000), gasUsed: 500_000_001, elapsed: second,
			want: *uint256.NewInt(1_001),
		},
		{
			name:   "a zero fee above target rises to one wei",
			params: noFloor, scheduled: uint256.Int{}, gasUsed: 710_000_000, elapsed: second,
			want: *uint256.NewInt(1),
		},
		{
			name:   "a zero fee below target stays zero",
			params: noFloor, scheduled: uint256.Int{}, gasUsed: 290_000_000, elapsed: second,
			want: uint256.Int{},
		},
		{
			name:   "a decrease rounds down",
			params: noFloor, scheduled: *uint256.NewInt(1_000), gasUsed: 0, elapsed: second,
			want: *uint256.NewInt(994),
		},
		{
			name:   "a scheduled fee below the floor is raised to it",
			params: example, scheduled: gwei(50), gasUsed: 500_000_000, elapsed: second,
			want: gwei(100),
		},
		{
			name:   "an increase past the largest uint256 saturates",
			params: example, scheduled: maxFee, gasUsed: 1_000_000_000, elapsed: second,
			want: maxFee,
		},
		{
			name:   "a change larger than the largest uint256 saturates",
			params: launch, scheduled: maxFee, gasUsed: 2_000_000_000, elapsed: 1_000,
			want: maxFee,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.params.NextBaseFee(tc.scheduled, tc.gasUsed, tc.elapsed)
			require.Equal(t, tc.want.Dec(), got.Dec())
		})
	}
}
