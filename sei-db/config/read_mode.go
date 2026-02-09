package config

import "fmt"

// ReadMode defines how EVM data reads are routed between backends.
type ReadMode string

const (
	// CosmosOnlyRead reads all data from Cosmos (memiavl) only.
	// This is the default/legacy behavior.
	CosmosOnlyRead ReadMode = "cosmos_only"

	// EVMFirstRead reads EVM data from EVM backend first, falls back to Cosmos if not found.
	// Use during migration to test EVM backend reads while maintaining Cosmos as fallback.
	EVMFirstRead ReadMode = "evm_first"

	// SplitRead reads EVM data from EVM backend and non-EVM data from Cosmos.
	// Use when migration is complete and backends are fully separated.
	SplitRead ReadMode = "split_read"
)

// IsValid returns true if the read mode is a recognized value
func (m ReadMode) IsValid() bool {
	switch m {
	case CosmosOnlyRead, EVMFirstRead, SplitRead:
		return true
	default:
		return false
	}
}

// ParseReadMode converts a string to a ReadMode, returning an error if invalid
func ParseReadMode(s string) (ReadMode, error) {
	m := ReadMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("invalid read mode: %q, valid modes: %s, %s, %s",
			s, CosmosOnlyRead, EVMFirstRead, SplitRead)
	}
	return m, nil
}
