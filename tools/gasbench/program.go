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
// fresh in-memory StateDB.
//
// This does NOT use runtime.Execute. Execute discards leftOverGas (it only
// surfaces gas via a tracer's OnTxEnd hook, and we run tracer-free -
// EVMConfig.Tracer == nil, the debug=false path), so it cannot return gas.
// runtime.Call is the identical fresh-EVM, tracer-free code path but returns
// leftOverGas directly. We replicate Execute's fresh-state setup
// (state.New + CreateAccount + SetCode) so the "fresh in-memory StateDB,
// stateless" property this MVP relies on is preserved.
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
// hot Call, not StateDB construction (which would swamp a nanosecond-scale
// opcode signal). Build one Program per bytecode input; call Run in the
// timing loop.
//
// Reusing a single StateDB across Run calls is safe here BECAUSE the
// benchmark programs are pure compute (no SSTORE/LOG/CREATE): they make no
// persistent state change, and runtime.Call resets transient storage and the
// access list on every entry via statedb.Prepare. Do not reuse a Program for
// state-touching code.
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

// Run executes the loaded code once, returning EVM gas consumed. This is the
// hot function for the timing loop: it does no per-call state allocation.
//
// gasUsed is cfg.GasLimit - leftOverGas: total EVM gas the interpreter
// charged. There is deliberately NO Cosmos/Sei gas meter in this path -
// runtime.Call builds a bare go-ethereum EVM with no ante handler and no
// sei-cosmos GasMeter. Isolating the EVM interpreter from the Cosmos layer is
// the entire point of the stateless-compute MVP.
//
// err is nil on a clean STOP/RETURN. On REVERT err is vm.ErrExecutionReverted
// (gasUsed is partial - unused gas is refunded on revert). On out-of-gas err
// is vm.ErrOutOfGas (gasUsed == GasLimit). On a bad/undefined opcode err is
// vm.ErrInvalidOpCode. The differential programs built in programs.go
// terminate cleanly (STOP, balanced stack), so a non-nil err means the run
// is INVALID - the caller must discard the sample, never treat it as a
// measurement. Matches the gasbench.RunOnce contract (gasbench.go).
func (p *Program) Run() (gasUsed uint64, err error) {
	_, leftOverGas, callErr := runtime.Call(contractAddr, nil, p.cfg)
	return p.cfg.GasLimit - leftOverGas, callErr
}
