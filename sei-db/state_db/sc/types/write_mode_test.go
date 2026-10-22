package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var allModes = []WriteMode{
	MemiavlOnly,
	MigrateEVM,
	EVMMigrated,
	MigrateCosmos,
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
// modes. Exactly four edges are legal: the steady-state -> migration
// steps and the migration -> completion steps. Everything else,
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
		{MemiavlOnly, MigrateEVM}:    true,
		{EVMMigrated, MigrateCosmos}: true,
		{MigrateEVM, EVMMigrated}:    true,
		{MigrateCosmos, FlatKVOnly}:  true,
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

// TestChainDistanceExhaustive pins that ChainDistance is the signed index
// difference along the migration chain for every pair of chain modes, and
// that any pair with a mode off the chain reports ok == false.
func TestChainDistanceExhaustive(t *testing.T) {
	index := map[WriteMode]int{}
	for i, mode := range migrationChain {
		index[mode] = i
	}

	onChainPairs := 0
	for _, from := range allModes {
		for _, to := range allModes {
			distance, ok := ChainDistance(from, to)
			fromIdx, fromOnChain := index[from]
			toIdx, toOnChain := index[to]
			if !fromOnChain || !toOnChain {
				require.False(t, ok, "ChainDistance(%q, %q) must report off-chain", from, to)
				continue
			}
			require.True(t, ok, "ChainDistance(%q, %q)", from, to)
			require.Equal(t, toIdx-fromIdx, distance, "ChainDistance(%q, %q)", from, to)
			onChainPairs++
		}
	}
	require.Equal(t, len(migrationChain)*len(migrationChain), onChainPairs)
}

// TestIsMigrationMode pins the modes whose exit transitions require a
// completed migration (the second half of the SetWriteMode safety check).
func TestIsMigrationMode(t *testing.T) {
	migrationModes := map[WriteMode]bool{
		MigrateEVM:    true,
		MigrateCosmos: true,
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
