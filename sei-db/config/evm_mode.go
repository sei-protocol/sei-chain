package config

import "fmt"

// EVMMode controls how EVM state is stored and served in the State Store (SS) layer.
//
// The two modes are intentionally the only options — earlier iterations supported
// dual_write / evm_first fallback combinations, but they masked inconsistencies
// between cosmos and evm backends (e.g. iteration silently succeeding on one
// while reads went to the other). Collapsing to cosmos_only vs. split removes
// the ambiguity: there is no fallback. A read/iterate that would miss returns
// empty rather than silently routing to a different backend.
type EVMMode string

const (
	// EVMModeCosmosOnly keeps all state — including EVM — in the Cosmos SS backend.
	// The EVM sub-store is not opened.
	EVMModeCosmosOnly EVMMode = "cosmos_only"

	// EVMModeSplit routes EVM data exclusively to the EVM SS backend and non-EVM
	// data exclusively to the Cosmos SS backend. Reads, writes, iteration, and
	// imports all obey this split — no cross-backend fallback.
	EVMModeSplit EVMMode = "split"
)

// IsValid returns true if the mode is a recognized value.
func (m EVMMode) IsValid() bool {
	switch m {
	case EVMModeCosmosOnly, EVMModeSplit:
		return true
	default:
		return false
	}
}

// ParseEVMMode converts a string to an EVMMode, returning an error if invalid.
// The empty string is treated as the default (cosmos_only).
func ParseEVMMode(s string) (EVMMode, error) {
	if s == "" {
		return EVMModeCosmosOnly, nil
	}
	m := EVMMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("invalid evm-ss-mode %q: expected %q or %q", s, EVMModeCosmosOnly, EVMModeSplit)
	}
	return m, nil
}
