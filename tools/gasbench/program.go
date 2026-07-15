package gasbench

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
)

// contractAddr matches the address runtime.Execute uses internally, for
// parity with the fork's own test harness.
var contractAddr = common.BytesToAddress([]byte("contract"))

// newRuntimeConfig builds a fresh, tracer-free interpreter config against a
// fresh in-memory StateDB. Uses runtime.Call, not runtime.Execute: Execute
// discards leftOverGas (see Program.Run). Replicates Execute's fresh-state
// setup (state.New + CreateAccount + SetCode).
func newRuntimeConfig(code []byte) (*runtime.Config, error) {
	cfg := new(runtime.Config)
	runtime.SetDefaults(cfg) // Shanghai+Cancun active, GasLimit=MaxUint64, no tracer
	cfg.EVMConfig = vm.Config{}

	sdb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		return nil, fmt.Errorf("gasbench: new statedb: %w", err)
	}
	cfg.State = sdb
	cfg.State.CreateAccount(contractAddr)
	cfg.State.SetCode(contractAddr, code)
	return cfg, nil
}

// Program holds a warmed EVM environment so a timing loop measures only the
// hot Call, not StateDB construction. Build one Program per bytecode input;
// call Run in the timing loop. See README.md "Program reuse across calls"
// before reusing a Program across a much longer or state-touching series.
type Program struct {
	cfg *runtime.Config
}

// NewProgram loads code into a warmed environment and validates that it runs
// clean once before any timing. A program that reverts/OOGs is rejected here
// so the timing loop never records a bogus sample.
func NewProgram(code []byte) (*Program, error) {
	cfg, err := newRuntimeConfig(code)
	if err != nil {
		return nil, err
	}
	p := &Program{cfg: cfg}
	if _, err := p.Run(); err != nil {
		return nil, fmt.Errorf("gasbench: program does not run clean: %w", err)
	}
	return p, nil
}

// Run executes the loaded code once through a bare go-ethereum EVM (no
// Cosmos ante handler or GasMeter -- see README.md "Gas isolation from the
// Cosmos layer") and returns EVM gas consumed (cfg.GasLimit - leftOverGas).
//
// err is nil on a clean STOP/RETURN, vm.ErrExecutionReverted on REVERT
// (gasUsed partial), vm.ErrOutOfGas on out-of-gas (gasUsed == GasLimit), or
// vm.ErrInvalidOpCode on a bad opcode. Every Case built by programs.go
// terminates cleanly, so a non-nil err means the run is invalid -- see the
// RunOnce contract in gasbench.go.
func (p *Program) Run() (gasUsed uint64, err error) {
	_, leftOverGas, callErr := runtime.Call(contractAddr, nil, p.cfg)
	return p.cfg.GasLimit - leftOverGas, callErr
}
