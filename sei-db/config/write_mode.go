package config

import "fmt"

// WriteMode defines how EVM data writes are routed between backends.
type WriteMode string

const (
	// CosmosOnlyWrite writes all data to Cosmos (memiavl) only.
	// This is the default/legacy behavior - no EVM backend involvement.
	CosmosOnlyWrite WriteMode = "cosmos_only"

	// DualWrite writes EVM data to both Cosmos and EVM backends.
	// Use during migration to populate the EVM backend while keeping Cosmos as source of truth.
	DualWrite WriteMode = "dual_write"

	// SplitWrite writes EVM data to EVM backend and non-EVM data to Cosmos.
	// Use when EVM migration is complete and backends are fully separated.
	SplitWrite WriteMode = "split_write"
)

// IsValid returns true if the write mode is a recognized value
func (m WriteMode) IsValid() bool {
	switch m {
	case CosmosOnlyWrite, DualWrite, SplitWrite:
		return true
	default:
		return false
	}
}

// ParseWriteMode converts a string to a WriteMode, returning an error if invalid
func ParseWriteMode(s string) (WriteMode, error) {
	m := WriteMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("invalid write mode: %s", s)
	}
	return m, nil
}
