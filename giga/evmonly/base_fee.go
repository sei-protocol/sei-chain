package evmonly

import (
	"github.com/holiman/uint256"
)

// BaseFeeAdjustmentOne is a base fee adjustment of 100%: adjustments are fixed point with 18
// decimal places.
var BaseFeeAdjustmentOne = uint256.NewInt(1_000_000_000_000_000_000)

// BaseFeeParams are the rules that move the base fee from one scheduled block to the next. Each
// adjustment is a fraction of the base fee, fixed point with 18 decimal places, at most
// BaseFeeAdjustmentOne.
type BaseFeeParams struct {
	// MaxIncrease is the adjustment a block using its whole gas limit raises the base fee by.
	MaxIncrease uint256.Int
	// MaxDecrease is the adjustment a block using no gas lowers the base fee by.
	MaxDecrease uint256.Int
	// MinBaseFee is the lowest base fee NextBaseFee returns, in wei.
	MinBaseFee uint256.Int
}

// NextBaseFee returns the base fee scheduled after scheduled, the latest one already set, when the
// block whose gas use sets it used gasUsed of its gasLimit. The block's target is half its gas
// limit. Above the target the fee rises by MaxIncrease times the share of the gas between the
// target and the limit that the block used; below it the fee falls by MaxDecrease times the share
// of the target the block left unused. The change is rounded down to the wei, an increase is at
// least one wei, and a result past the largest uint256 saturates. A block without a gas limit
// leaves scheduled as it is. The result is never below MinBaseFee.
func (p BaseFeeParams) NextBaseFee(scheduled uint256.Int, gasUsed, gasLimit uint64) uint256.Int {
	return p.Hold(p.adjust(scheduled, gasUsed, gasLimit))
}

// Hold returns the base fee scheduled after scheduled when no block's gas use moves it: scheduled,
// or MinBaseFee when that is higher.
func (p BaseFeeParams) Hold(scheduled uint256.Int) uint256.Int {
	if scheduled.Lt(&p.MinBaseFee) {
		return p.MinBaseFee
	}
	return scheduled
}

// adjust returns scheduled moved by the gas use of the block that sets the next base fee.
func (p BaseFeeParams) adjust(scheduled uint256.Int, gasUsed, gasLimit uint64) uint256.Int {
	if gasLimit == 0 {
		return scheduled
	}
	// With the target at half the limit, either share is |2 × used − limit| / limit, which stays
	// exact for an odd limit.
	limit := uint256.NewInt(gasLimit)
	var doubled, share uint256.Int
	doubled.Mul(uint256.NewInt(min(gasUsed, gasLimit)), uint256.NewInt(2))
	switch doubled.Cmp(limit) {
	case 1:
		share.Sub(&doubled, limit)
		return raise(&scheduled, &p.MaxIncrease, &share, limit)
	case -1:
		share.Sub(limit, &doubled)
		return lower(&scheduled, &p.MaxDecrease, &share, limit)
	default:
		return scheduled
	}
}

// raise returns scheduled raised by adjustment × share / limit of itself, by at least one wei,
// saturating past the largest uint256.
func raise(scheduled, adjustment, share, limit *uint256.Int) uint256.Int {
	var next uint256.Int
	change, overflow := baseFeeChange(scheduled, adjustment, share, limit)
	if overflow {
		return *next.SetAllOne()
	}
	if change.IsZero() {
		change.SetOne()
	}
	if _, overflow := next.AddOverflow(scheduled, &change); overflow {
		next.SetAllOne()
	}
	return next
}

// lower returns scheduled lowered by adjustment × share / limit of itself, never below zero.
func lower(scheduled, adjustment, share, limit *uint256.Int) uint256.Int {
	var next uint256.Int
	change, overflow := baseFeeChange(scheduled, adjustment, share, limit)
	if overflow || !change.Lt(scheduled) {
		return next
	}
	return *next.Sub(scheduled, &change)
}

// baseFeeChange returns scheduled × adjustment × share / (limit × BaseFeeAdjustmentOne), rounded
// down, and whether it overflowed.
func baseFeeChange(scheduled, adjustment, share, limit *uint256.Int) (uint256.Int, bool) {
	var numerator, denominator, change uint256.Int
	if _, overflow := numerator.MulOverflow(adjustment, share); overflow {
		return change, true
	}
	denominator.Mul(limit, BaseFeeAdjustmentOne) // below 2^124
	_, overflow := change.MulDivOverflow(scheduled, &numerator, &denominator)
	return change, overflow
}
