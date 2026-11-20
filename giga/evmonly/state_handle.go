package evmonly

import "github.com/ethereum/go-ethereum/core/vm"

// StateHandle is an EVM state snapshot that must be closed after use.
type StateHandle struct {
	vm.SeiStateDB
	release func()
}

// NewStateHandle returns a state snapshot with its release function.
func NewStateHandle(state vm.SeiStateDB, release func()) *StateHandle {
	return &StateHandle{SeiStateDB: state, release: release}
}

// Close releases the snapshot. The state must not be used afterward.
func (s *StateHandle) Close() { s.release() }
