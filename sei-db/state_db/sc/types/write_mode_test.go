package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var allModes = []WriteMode{
	MemiavlOnly,
	MigrateEVM,
	EVMMigrated,
	MigrateAllButBank,
	AllMigratedButBank,
	MigrateBank,
	FlatKVOnly,
	TestOnlyDualWrite,
	Auto,
}

func TestWriteModeIsValid(t *testing.T) {
	for _, m := range allModes {
		require.True(t, m.IsValid(), "mode %q should be valid", m)
	}
	require.False(t, WriteMode("bogus").IsValid())
	require.False(t, WriteMode("").IsValid())
}

func TestParseWriteMode(t *testing.T) {
	for _, m := range allModes {
		parsed, err := ParseWriteMode(string(m))
		require.NoError(t, err)
		require.Equal(t, m, parsed)
	}
	_, err := ParseWriteMode("bogus")
	require.Error(t, err)
}

// TestValidateTransitionExhaustive checks every (from, to) pair of write
// modes. Exactly six edges are legal: the three steady-state -> migration
// steps and the three migration -> completion steps. Everything else,
// including same-mode pairs (handled as no-ops by callers before invoking
// ValidateTransition), must be rejected.
//
// Whether a legal transition additionally requires a completed migration
// is not ValidateTransition's job: callers consult from.IsMigrationMode(),
// covered by TestIsMigrationMode below.
func TestValidateTransitionExhaustive(t *testing.T) {
	type edge struct {
		from WriteMode
		to   WriteMode
	}
	legal := map[edge]bool{
		{MemiavlOnly, MigrateEVM}:               true,
		{EVMMigrated, MigrateAllButBank}:        true,
		{AllMigratedButBank, MigrateBank}:       true,
		{MigrateEVM, EVMMigrated}:               true,
		{MigrateAllButBank, AllMigratedButBank}: true,
		{MigrateBank, FlatKVOnly}:               true,
	}

	legalSeen := 0
	for _, from := range allModes {
		for _, to := range allModes {
			err := ValidateTransition(from, to)
			if legal[edge{from: from, to: to}] {
				require.NoError(t, err, "transition %q -> %q should be legal", from, to)
				legalSeen++
			} else {
				require.Error(t, err, "transition %q -> %q should be illegal", from, to)
			}
		}
	}
	require.Equal(t, len(legal), legalSeen)
}

// TestIsMigrationMode pins the modes whose exit transitions require a
// completed migration (the second half of the SetWriteMode safety check).
func TestIsMigrationMode(t *testing.T) {
	migrationModes := map[WriteMode]bool{
		MigrateEVM:        true,
		MigrateAllButBank: true,
		MigrateBank:       true,
	}
	for _, mode := range allModes {
		require.Equal(t, migrationModes[mode], mode.IsMigrationMode(), "IsMigrationMode(%q)", mode)
	}
}

func TestValidateTransitionInvalidTargets(t *testing.T) {
	for _, from := range allModes {
		require.Error(t, ValidateTransition(from, Auto),
			"Auto must never be a transition target (from %q)", from)
		require.Error(t, ValidateTransition(from, TestOnlyDualWrite),
			"TestOnlyDualWrite must never be a transition target (from %q)", from)
		require.Error(t, ValidateTransition(from, WriteMode("bogus")),
			"unknown modes must never be a transition target (from %q)", from)
	}
}
