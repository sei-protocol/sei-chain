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

// adjustment returns a base fee adjustment of thousandths/1000.
func adjustment(thousandths uint64) uint256.Int {
	return *uint256.NewInt(thousandths * 1_000_000_000_000_000)
}

const gasLimit = uint64(1_000_000_000)

func TestNextBaseFee(t *testing.T) {
	eip1559 := BaseFeeParams{MaxIncrease: adjustment(125), MaxDecrease: adjustment(125)}
	launch := BaseFeeParams{MaxIncrease: adjustment(6), MaxDecrease: adjustment(6)}
	// v2's default adjustments, 1.89% up and 0.39% down.
	asymmetric := BaseFeeParams{
		MaxIncrease: *uint256.NewInt(18_900_000_000_000_000),
		MaxDecrease: *uint256.NewInt(3_900_000_000_000_000),
	}
	var maxFee uint256.Int
	maxFee.SetAllOne()

	for _, tc := range []struct {
		name      string
		params    BaseFeeParams
		scheduled uint256.Int
		gasUsed   uint64
		gasLimit  uint64
		want      uint256.Int
	}{
		{
			name:   "a full block raises the fee by MaxIncrease",
			params: eip1559, scheduled: gwei(120), gasUsed: gasLimit, gasLimit: gasLimit,
			want: gwei(135),
		},
		{
			name:   "an empty block lowers the fee by MaxDecrease",
			params: eip1559, scheduled: gwei(120), gasUsed: 0, gasLimit: gasLimit,
			want: gwei(105),
		},
		{
			name:   "half the gas limit leaves the fee unchanged",
			params: eip1559, scheduled: gwei(120), gasUsed: gasLimit / 2, gasLimit: gasLimit,
			want: gwei(120),
		},
		{
			name:   "halfway from the target to the limit raises the fee by half of MaxIncrease",
			params: eip1559, scheduled: gwei(120), gasUsed: 750_000_000, gasLimit: gasLimit,
			want: *uint256.NewInt(127_500_000_000),
		},
		{
			name:   "a full block at launch raises the fee by 0.6%",
			params: launch, scheduled: gwei(1_000), gasUsed: gasLimit, gasLimit: gasLimit,
			want: gwei(1_006),
		},
		{
			name:   "an empty block at launch lowers the fee by 0.6%",
			params: launch, scheduled: gwei(1_000), gasUsed: 0, gasLimit: gasLimit,
			want: gwei(994),
		},
		{
			name:   "a full block raises by its own adjustment",
			params: asymmetric, scheduled: gwei(100), gasUsed: gasLimit, gasLimit: gasLimit,
			want: *uint256.NewInt(101_890_000_000),
		},
		{
			name:   "an empty block lowers by its own adjustment",
			params: asymmetric, scheduled: gwei(100), gasUsed: 0, gasLimit: gasLimit,
			want: *uint256.NewInt(99_610_000_000),
		},
		{
			name:   "halfway from the target to the limit raises by half the upward adjustment",
			params: asymmetric, scheduled: gwei(100), gasUsed: 750_000_000, gasLimit: gasLimit,
			want: *uint256.NewInt(100_945_000_000),
		},
		{
			name:   "halfway from empty to the target lowers by half the downward adjustment",
			params: asymmetric, scheduled: gwei(100), gasUsed: 250_000_000, gasLimit: gasLimit,
			want: *uint256.NewInt(99_805_000_000),
		},
		{
			name:   "gas past the limit counts as a full block",
			params: launch, scheduled: gwei(1_000), gasUsed: 2 * gasLimit, gasLimit: gasLimit,
			want: gwei(1_006),
		},
		{
			name:   "an odd gas limit's target is exactly half of it",
			params: eip1559, scheduled: gwei(24), gasUsed: 2, gasLimit: 3,
			want: gwei(25),
		},
		{
			name:   "a block without a gas limit leaves the fee unchanged",
			params: launch, scheduled: gwei(1_000), gasUsed: 21_000, gasLimit: 0,
			want: gwei(1_000),
		},
		{
			name:   "an increase rounding to zero is one wei",
			params: launch, scheduled: *uint256.NewInt(100), gasUsed: gasLimit, gasLimit: gasLimit,
			want: *uint256.NewInt(101),
		},
		{
			name:   "a zero fee above target rises to one wei",
			params: launch, scheduled: uint256.Int{}, gasUsed: gasLimit, gasLimit: gasLimit,
			want: *uint256.NewInt(1),
		},
		{
			name:   "a zero fee below target stays zero",
			params: launch, scheduled: uint256.Int{}, gasUsed: 0, gasLimit: gasLimit,
			want: uint256.Int{},
		},
		{
			name:   "a decrease rounds down",
			params: launch, scheduled: *uint256.NewInt(1_001), gasUsed: 0, gasLimit: gasLimit,
			want: *uint256.NewInt(995),
		},
		{
			name:   "a decrease of 100% lowers the fee to zero",
			params: BaseFeeParams{MaxDecrease: *BaseFeeAdjustmentOne}, scheduled: gwei(120), gasUsed: 0, gasLimit: gasLimit,
			want: uint256.Int{},
		},
		{
			name:   "an increase past the largest uint256 saturates",
			params: eip1559, scheduled: maxFee, gasUsed: gasLimit, gasLimit: gasLimit,
			want: maxFee,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.params.NextBaseFee(tc.scheduled, tc.gasUsed, tc.gasLimit)
			require.Equal(t, tc.want.Dec(), got.Dec())
		})
	}
}
