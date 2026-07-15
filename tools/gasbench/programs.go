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
	Baseline         []byte
	Target           []byte
	ExpectedGasDelta uint64
}

// seed256 is the fixed working-value operand: full-width (2^256-1) so
// magnitude-dependent ops exercise their full uint256 limb path.
var seed256 = new(uint256.Int).Not(uint256.NewInt(0))

// seedShift is a fixed, in-range (<256) shift amount for SHL/SHR/SAR --
// see README.md "Differential construction" for why this must differ from
// seed256.
var seedShift = uint256.NewInt(4)

// BuildCases builds every spec's case at DefaultReps.
func BuildCases() []Case {
	out := make([]Case, 0, len(Specs))
	for _, s := range Specs {
		out = append(out, BuildCaseWith(s, DefaultReps, seed256))
	}
	return out
}

// BuildCaseWith constructs the differential case for one opcode. See
// README.md "Differential construction" for the balanced-DUP/POP algebra;
// per-branch delta formulas are inlined below next to the code they justify.
// Every unit is net-0 so stack depth never grows or underflows.
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
		// DUP2 (not DUP1 x2, unlike the default branch below): these ops
		// need two DISTINCT operands, not n copies of one value -- see
		// README.md "Differential construction".
		base.Push(seed).Push(seedShift)
		tgt.Push(seed).Push(seedShift)
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
