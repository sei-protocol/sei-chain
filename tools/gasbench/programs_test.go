package gasbench

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
)

// TestBuildCaseWithPanicsOnUnhandledArityZero pins the default branch's
// Arity guard: an Arity-0 spec that is not special-cased (unlike DUP1/SWAP1)
// must panic rather than silently feed n=0 into the (n-1)*GasQuickStep
// delta formula.
func TestBuildCaseWithPanicsOnUnhandledArityZero(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("BuildCaseWith accepted an Arity-0 spec in the default branch; want panic")
		}
	}()
	BuildCaseWith(OpSpec{Name: "PC", Op: vm.PC, Class: ClassStack, Arity: 0, ConstGas: vm.GasQuickStep}, 10, seedOperands)
}
