package gasbench

import (
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/program"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// DefaultReps is the number of unrolled body units per program. Straight-line
// (no loop counter) keeps the interpreter dispatch loop identical between
// baseline and target; the count is fixed and equal for both, so loop-control
// cost cannot leak into the differential. 1000 gives a strong signal while
// staying well under any code/gas limit.
const DefaultReps = 1000

// Class groups opcodes by arithmetic character.
type Class string

const (
	ClassArithmetic Class = "arithmetic"
	ClassBitwise    Class = "bitwise"
	ClassComparison Class = "comparison"
	ClassStack      Class = "stack"
)

// OpSpec is one benchmarkable opcode.
type OpSpec struct {
	Name     string
	Op       vm.OpCode
	Class    Class
	Arity    int    // stack items consumed by the op (operands)
	ConstGas uint64 // definitional constant gas from the fork jump table
	// DataDependent is true when execution TIME varies with operand
	// magnitude (uint256 limb-count loops) even though gas is constant.
	// Treat these as curves, not points, if the harness ever sweeps operands
	// (deferred - see the design's Non-goals).
	DataDependent bool
}

// Specs is the benchmarkable scalar-opcode set. Every entry is CONSTANT-gas
// in this fork (verified against core/vm/jump_table.go + core/vm/eips.go);
// none has dynamic gas, none touches memory/state. EXP is deliberately
// omitted: its gas is parametric (gasExpFrontier over exponent byte length),
// so it does not belong in a constant-gas differential.
//
// No scalar opcode here is repriced on Sei: production always builds the EVM
// with a stock vm.Config{} (x/evm/keeper/evm.go, msg_server.go, ante/fee.go),
// no custom jump table, no opcode gas overrides. The only Sei gas param is
// SeiSstoreSetGasEIP2200 (x/evm/types/params.go), a storage opcode and out of
// scope here. If a scalar opcode is ever repriced, its gas must come from the
// live x/evm params, not this table's geth constant.
var Specs = []OpSpec{
	// arithmetic
	{"ADD", vm.ADD, ClassArithmetic, 2, vm.GasFastestStep, false},
	{"MUL", vm.MUL, ClassArithmetic, 2, vm.GasFastStep, true},
	{"SUB", vm.SUB, ClassArithmetic, 2, vm.GasFastestStep, false},
	{"DIV", vm.DIV, ClassArithmetic, 2, vm.GasFastStep, true},
	{"MOD", vm.MOD, ClassArithmetic, 2, vm.GasFastStep, true},
	{"ADDMOD", vm.ADDMOD, ClassArithmetic, 3, vm.GasMidStep, true},
	{"MULMOD", vm.MULMOD, ClassArithmetic, 3, vm.GasMidStep, true},
	// bitwise
	{"AND", vm.AND, ClassBitwise, 2, vm.GasFastestStep, false},
	{"OR", vm.OR, ClassBitwise, 2, vm.GasFastestStep, false},
	{"XOR", vm.XOR, ClassBitwise, 2, vm.GasFastestStep, false},
	{"NOT", vm.NOT, ClassBitwise, 1, vm.GasFastestStep, false},
	{"SHL", vm.SHL, ClassBitwise, 2, vm.GasFastestStep, true},
	{"SHR", vm.SHR, ClassBitwise, 2, vm.GasFastestStep, true},
	{"SAR", vm.SAR, ClassBitwise, 2, vm.GasFastestStep, true},
	// comparison
	{"LT", vm.LT, ClassComparison, 2, vm.GasFastestStep, false},
	{"GT", vm.GT, ClassComparison, 2, vm.GasFastestStep, false},
	{"SLT", vm.SLT, ClassComparison, 2, vm.GasFastestStep, false},
	{"SGT", vm.SGT, ClassComparison, 2, vm.GasFastestStep, false},
	{"EQ", vm.EQ, ClassComparison, 2, vm.GasFastestStep, false},
	{"ISZERO", vm.ISZERO, ClassComparison, 1, vm.GasFastestStep, false},
	// stack
	{"DUP1", vm.DUP1, ClassStack, 0, vm.GasFastestStep, false},
	{"SWAP1", vm.SWAP1, ClassStack, 0, vm.GasFastestStep, false},
}

// Case is a differential bytecode pair for one opcode, ready for measurement.
// Baseline and target are identical except that target executes the opcode;
// both run the same fixed unit count and both terminate cleanly on STOP with
// a balanced stack.
//
// ExpectedGasDelta is the definitional, whole-program gas the opcode adds
// (per-unit delta * Reps) - NOT necessarily Reps*ConstGas, because a net-0,
// cleanly-looping body needs stack rebalancing (see BuildPairWith). It is the
// expected value; the measured Diff.GasDelta from a live run is the ground
// truth, and the two should be cross-checked (see bench_test.go) as a
// self-check of the differential construction itself.
type Case struct {
	OpcodeID         string
	Class            Class
	DataDependent    bool
	Reps             int
	Baseline         []byte
	Target           []byte
	ExpectedGasDelta uint64
}

// seed256 is the fixed operand pushed once as the working value. Full-width
// (2^256-1) so the magnitude-dependent ops (MUL/DIV/MOD/ADDMOD/MULMOD)
// exercise their full uint256 limb path rather than a small-number fast
// case. Exposed via BuildCaseWith for operand sweeps.
var seed256 = new(uint256.Int).Not(uint256.NewInt(0))

// seedShift is a fixed, in-range (<256) shift amount for SHL/SHR/SAR. A
// shift amount >= 256 takes go-ethereum's value.Clear() early-out (the
// degenerate, cheapest case: the result is zero without touching the
// value's limbs), so reusing seed256 as BOTH operands -- as the generic
// same-value construction below does for every other opcode -- measures
// that early-out, not representative shift work. Kept separate from the
// value operand so the shift ops still exercise the interpreter's limb-shift
// path on a full-width value.
var seedShift = uint256.NewInt(4)

// BuildCases builds every spec's case at DefaultReps.
func BuildCases() []Case {
	out := make([]Case, 0, len(Specs))
	for _, s := range Specs {
		out = append(out, BuildCaseWith(s, DefaultReps, seed256))
	}
	return out
}

// BuildCaseWith constructs the differential case for one opcode.
//
// Construction (the balanced-DUP/POP differential, the fork's
// benchmarkNonModifyingCode technique adapted to a bounded, clean-terminating
// straight line). For an op consuming n operands and producing 1 result, the
// net-0 repeating unit is:
//
//	target   = DUP1 x n , OP     , POP        // dup n copies of top, run op, drop result
//	baseline = DUP1 x n , POP x n             // dup n copies of top, drop them
//
// The n DUP1s are identical on both sides and cancel; one POP cancels the
// target's trailing POP. What remains -- the whole differential -- is the op
// vs (n-1) extra POPs, so the per-unit gas delta = ConstGas(op) - (n-1)*GasQuickStep.
//   - n=1 (NOT, ISZERO): delta = ConstGas(op)               (op isolated exactly)
//   - n=2 (most ops):    delta = ConstGas(op) - 2
//   - n=3 (ADDMOD/MULMOD): delta = ConstGas(op) - 4
//
// Stack ops are special-cased (not "n operands -> 1 result"):
//   - DUP1  (1->2): target = DUP1 POP ; baseline = PUSH0 POP -> delta = 3-2 = 1
//   - SWAP1 (2->2): target = SWAP1     ; baseline = JUMPDEST  -> delta = 3-1 = 2
//
// SHL/SHR/SAR are also special-cased: they need two DISTINCT operands (see
// seedShift), not "n copies of the same value". The prologue pushes
// (seed, seedShift); DUP2 reaches two positions down without disturbing the
// other operand, so the SAME (value, shift) pair is reused net-0 across
// every unit:
//
//	target   = DUP2, DUP2, OP , POP    // dup shift then value, run op, drop result
//	baseline = DUP2, DUP2, POP, POP    // dup shift then value, drop both
//
// This yields the same arity=2 shape as the general case:
// delta = ConstGas(op) - GasQuickStep (one extra POP in baseline vs target).
//
// Every case's prologue seeds its own stack, and every unit is net-0 so stack
// depth never grows (no 1024-depth risk) and never underflows.
func BuildCaseWith(s OpSpec, reps int, seed *uint256.Int) Case {
	base := program.New()
	tgt := program.New()

	var perUnitDelta uint64
	switch s.Op {
	case vm.DUP1:
		base.Push(seed).Push(seed)
		tgt.Push(seed).Push(seed)
		for i := 0; i < reps; i++ {
			tgt.Op(vm.DUP1, vm.POP)
			base.Op(vm.PUSH0, vm.POP)
		}
		perUnitDelta = s.ConstGas - vm.GasQuickStep // 3 - 2

	case vm.SWAP1:
		base.Push(seed).Push(seed)
		tgt.Push(seed).Push(seed)
		for i := 0; i < reps; i++ {
			tgt.Op(vm.SWAP1)
			base.Op(vm.JUMPDEST)
		}
		perUnitDelta = s.ConstGas - params.JumpdestGas // 3 - 1

	case vm.SHL, vm.SHR, vm.SAR:
		base.Push(seed).Push(seedShift)
		tgt.Push(seed).Push(seedShift)
		for i := 0; i < reps; i++ {
			tgt.Op(vm.DUP2, vm.DUP2, s.Op, vm.POP)
			base.Op(vm.DUP2, vm.DUP2, vm.POP, vm.POP)
		}
		perUnitDelta = s.ConstGas - vm.GasQuickStep // 3 - 2, same shape as the n=2 default case

	default:
		base.Push(seed).Push(seed)
		tgt.Push(seed).Push(seed)
		for i := 0; i < reps; i++ {
			for d := 0; d < s.Arity; d++ {
				tgt.Op(vm.DUP1)
				base.Op(vm.DUP1)
			}
			tgt.Op(s.Op, vm.POP)
			for d := 0; d < s.Arity; d++ {
				base.Op(vm.POP)
			}
		}
		perUnitDelta = s.ConstGas - uint64(s.Arity-1)*vm.GasQuickStep
	}

	base.Op(vm.STOP)
	tgt.Op(vm.STOP)

	return Case{
		OpcodeID:         s.Name,
		Class:            s.Class,
		DataDependent:    s.DataDependent,
		Reps:             reps,
		Baseline:         base.Bytes(),
		Target:           tgt.Bytes(),
		ExpectedGasDelta: perUnitDelta * uint64(reps),
	}
}
