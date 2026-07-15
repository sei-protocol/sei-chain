package gasbench

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/program"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// DefaultReps is the number of unrolled (straight-line, no loop counter)
// body units per program, fixed and equal for baseline and target.
const DefaultReps = 1000

// Class groups opcodes by arithmetic character.
type Class string

// Opcode classes.
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
	// magnitude even though gas is constant. See README.md "Scope".
	DataDependent bool
}

// Specs is the benchmarkable scalar-opcode set: every entry is
// constant-gas in this fork (verified against core/vm/jump_table.go +
// core/vm/eips.go), none touches memory/state. EXP is omitted: its gas is
// parametric (gasExpFrontier), not constant. See AGENTS.md for the
// hand-verification requirement when adding an entry.
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

// Case is a differential bytecode pair for one opcode, ready for
// measurement. See README.md "Differential construction".
//
// ExpectedGasDelta is the definitional whole-program gas delta from
// BuildCaseWith's algebra (not necessarily Reps*ConstGas -- stack
// rebalancing adds its own cost); bench_test.go cross-checks it against the
// measured Diff.GasDelta as a self-check of the construction itself.
type Case struct {
	OpcodeID         string
	Class            Class
	DataDependent    bool
	Reps             int
	ConstGas         uint64 // nominal per-op gas the chain charges (OpSpec.ConstGas); the repricing-relevant denominator, distinct from the differential GasDelta
	Baseline         []byte
	Target           []byte
	ExpectedGasDelta uint64
}

// seedOperands are the distinct, non-zero, ascending working-value operands
// fed to the general (default) branch -- one per stack slot the opcode
// consumes, index 0 landing at the modulus/divisor position and the last at
// the top of the stack.
//
// Distinctness and order are load-bearing, not cosmetic. Equal operands make
// holiman/uint256 short-circuit the arithmetic before the real kernel:
// DIV(x,x) returns 1 and MOD(x,x) returns 0 without ever calling udivrem
// (uint256.go Div/Mod), so an all-equal input would time a compare, not a
// 256-bit division, and understate DIV/MOD by ~10x. Ascending order keeps the
// arity-2 dividend (top) above the divisor so DIV/MOD reach udivrem, and puts
// the smallest value at the modulus slot for ADDMOD/MULMOD so their operands
// are not already reduced. All three are multi-limb; the largest is full-width.
// See README.md "Differential construction".
var seedOperands = []*uint256.Int{
	uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffff"),                 // 2^192-1 (divisor/modulus slot)
	uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),         // 2^224-1
	uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"), // 2^256-1 (full width)
}

// seedShift is a fixed, in-range (<256) shift amount for SHL/SHR/SAR -- it must
// be a small distinct operand, not a full-width value, or the shift would hit
// go-ethereum's "shift >= 256 -> 0" early-out. See README.md "Differential
// construction".
var seedShift = uint256.NewInt(4)

// BuildCases builds every spec's case at DefaultReps.
func BuildCases() []Case {
	out := make([]Case, 0, len(Specs))
	for _, s := range Specs {
		out = append(out, BuildCaseWith(s, DefaultReps, seedOperands))
	}
	return out
}

// BuildCaseWith constructs the differential case for one opcode. See
// README.md "Differential construction" for the balanced-DUP/POP algebra;
// per-branch delta formulas are inlined below next to the code they justify.
// Every unit is net-0 so stack depth never grows or underflows.
func BuildCaseWith(s OpSpec, reps int, operands []*uint256.Int) Case {
	base := program.New()
	tgt := program.New()

	// DUP1/SWAP1 need two values on the stack; the general branch needs one
	// distinct operand per slot the opcode consumes. Guard here so a caller
	// that under-supplies operands fails at construction, not with a silent
	// stack underflow at measurement.
	if len(operands) < 2 || len(operands) < s.Arity {
		panic(fmt.Sprintf("gasbench: %s needs >= max(2, arity=%d) operands, got %d", s.Name, s.Arity, len(operands)))
	}
	// Guard the conversions below: reps feeds uint64(reps) in ExpectedGasDelta
	// and Arity feeds the DUP<arity> opcode byte (DUP16 is the EVM ceiling).
	if reps <= 0 {
		panic(fmt.Sprintf("gasbench: %s needs reps > 0, got %d", s.Name, reps))
	}
	if s.Arity > 16 {
		panic(fmt.Sprintf("gasbench: %s has Arity %d > 16 (DUP16 is the deepest lift)", s.Name, s.Arity))
	}

	var perUnitDelta uint64
	switch s.Op {
	case vm.DUP1:
		base.Push(operands[0]).Push(operands[1])
		tgt.Push(operands[0]).Push(operands[1])
		for i := 0; i < reps; i++ {
			tgt.Op(vm.DUP1, vm.POP)
			base.Op(vm.PUSH0, vm.POP)
		}
		perUnitDelta = s.ConstGas - vm.GasQuickStep // 3 - 2

	case vm.SWAP1:
		base.Push(operands[0]).Push(operands[1])
		tgt.Push(operands[0]).Push(operands[1])
		for i := 0; i < reps; i++ {
			tgt.Op(vm.SWAP1)
			base.Op(vm.JUMPDEST)
		}
		perUnitDelta = s.ConstGas - params.JumpdestGas // 3 - 1

	case vm.SHL, vm.SHR, vm.SAR:
		// Shifts need a small in-range shift amount on top and a full-width
		// value beneath -- distinct operands, or the shift hits go-ethereum's
		// ">= 256 -> 0" early-out. This is the arity-2 shape of the default
		// branch with a bespoke top operand; see README.md "Differential
		// construction".
		value := operands[len(operands)-1] // full-width
		base.Push(value).Push(seedShift)
		tgt.Push(value).Push(seedShift)
		for i := 0; i < reps; i++ {
			tgt.Op(vm.DUP2, vm.DUP2, s.Op, vm.POP)
			base.Op(vm.DUP2, vm.DUP2, vm.POP, vm.POP)
		}
		perUnitDelta = s.ConstGas - vm.GasQuickStep // 3 - 2, same shape as the n=2 default case

	default:
		// General n-operand case: see README.md "Differential construction"
		// for the (n-1)*GasQuickStep derivation this formula implements.
		// Arity 0 must be special-cased above (like DUP1/SWAP1): the
		// (n-1)*GasQuickStep term below assumes n >= 1.
		if s.Arity < 1 {
			panic(fmt.Sprintf("gasbench: %s has Arity %d < 1 and is not special-cased in BuildCaseWith", s.Name, s.Arity))
		}
		// Push one DISTINCT operand per slot, then DUP<arity> reps times.
		// DUP<arity> copies the deepest of the arity operands; repeating it
		// arity times lifts fresh copies of all arity operands back to the top
		// in order, so every unit re-runs the op on the SAME distinct tuple
		// (equal operands would let uint256 short-circuit DIV/MOD -- see
		// seedOperands). GasFastestStep is identical for every DUP<k>, so this
		// leaves the (n-1)*GasQuickStep gas algebra unchanged from a DUP1 fill.
		dupN := vm.DUP1 + vm.OpCode(s.Arity-1) //nolint:gosec // 1 <= Arity <= 16 guarded above
		for d := 0; d < s.Arity; d++ {
			base.Push(operands[d])
			tgt.Push(operands[d])
		}
		for i := 0; i < reps; i++ {
			for d := 0; d < s.Arity; d++ {
				tgt.Op(dupN)
				base.Op(dupN)
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
		ConstGas:         s.ConstGas,
		Baseline:         base.Bytes(),
		Target:           tgt.Bytes(),
		ExpectedGasDelta: perUnitDelta * uint64(reps), //nolint:gosec // reps > 0 guarded above
	}
}
