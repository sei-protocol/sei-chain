package evmonly

import (
	"github.com/holiman/uint256"
)

// nanosPerSecond converts a gas rate per second to one per nanosecond.
var nanosPerSecond = uint256.NewInt(1_000_000_000)

// BaseFeeParams are the rules that move the base fee from one scheduled block to the next.
type BaseFeeParams struct {
	// Floor is the lowest base fee, in wei per gas.
	Floor uint256.Int
	// TargetGasPerSecond is the rate of gas use that leaves the base fee unchanged. A block's
	// target is this rate over the time since its parent.
	TargetGasPerSecond uint64
	// Denominator sets the step size: a block using twice its target raises the base fee by
	// 1/Denominator, and an empty block lowers it by as much. It must be positive.
	Denominator uint64
}

// NextBaseFee returns the base fee scheduled after scheduled, the latest one already set, when the
// block whose gas use sets it used gasUsed over elapsed nanoseconds since its parent:
// max(Floor, scheduled × (1 + (gasUsed − T) / (Denominator × T))) for that block's target T. The
// change is rounded down to the wei and an increase is at least one wei. A result past the largest
// uint256 saturates, and a block that takes no time leaves scheduled as it is, raised to Floor.
func (p BaseFeeParams) NextBaseFee(scheduled uint256.Int, gasUsed, elapsed uint64) uint256.Int {
	next := scheduled
	if elapsed > 0 {
		next = p.adjust(&scheduled, gasUsed, elapsed)
	}
	if next.Lt(&p.Floor) {
		next = p.Floor
	}
	return next
}

// adjust returns scheduled moved by the gas a block of elapsed nanoseconds used against its target.
func (p BaseFeeParams) adjust(scheduled *uint256.Int, gasUsed, elapsed uint64) uint256.Int {
	// Gas and the target are both counted in gas-nanoseconds per second, so the target is exact.
	var used, target, divisor uint256.Int
	used.Mul(uint256.NewInt(gasUsed), nanosPerSecond)
	target.Mul(uint256.NewInt(p.TargetGasPerSecond), uint256.NewInt(elapsed))
	divisor.Mul(&target, uint256.NewInt(p.Denominator))

	var next, delta, change uint256.Int
	switch used.Cmp(&target) {
	case 1:
		delta.Sub(&used, &target)
		if _, overflow := change.MulDivOverflow(scheduled, &delta, &divisor); overflow {
			return *next.SetAllOne()
		}
		if change.IsZero() {
			change.SetOne()
		}
		if _, overflow := next.AddOverflow(scheduled, &change); overflow {
			next.SetAllOne()
		}
	case -1:
		delta.Sub(&target, &used)
		change.MulDivOverflow(scheduled, &delta, &divisor) // at most scheduled / Denominator
		next.Sub(scheduled, &change)
	default:
		next.Set(scheduled)
	}
	return next
}
