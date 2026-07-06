package types

import "fmt"

// WriteMode defines how EVM data writes are routed between backends.
type WriteMode string

const (
	// MemiavlOnly writes all data to memiavl only.
	//
	// Migration version 0.
	MemiavlOnly WriteMode = "memiavl_only"

	// MigrateEVM migrates the evm/ module from memiavl to flatkv.
	//
	// Handles the transition from migration version 0 to 1,
	// and continues to function once we reach migration version 1.
	MigrateEVM WriteMode = "migrate_evm"

	// EVMMigrated is the steady state after the evm/ module has been migrated, but before we
	// are ready to do the next migration.
	//
	// Migration version 1.
	EVMMigrated WriteMode = "evm_migrated"

	// MigrateAllButBank migrates all but the bank module from memiavl to flatkv.
	//
	// Handles the transition from migration version 1 to 2,
	// and continues to function once we reach migration version 2.
	MigrateAllButBank WriteMode = "migrate_all_but_bank"

	// AllMigratedButBank is the steady state after all but the bank module has been migrated,
	// but before we are ready to do the next migration.
	//
	// Migration version 2.
	AllMigratedButBank WriteMode = "all_migrated_but_bank"

	// MigrateBank migrates the bank module from memiavl to flatkv.
	//
	// Handles the transition from migration version 2 to 3,
	// and continues to function once we reach migration version 3.
	MigrateBank WriteMode = "migrate_bank"

	// All data is written to FlatKV.
	//
	// Migration version 3.
	FlatKVOnly WriteMode = "flatkv_only"

	// TestOnlyDualWrite is a test-only dual-write router. EVM traffic is written to both memiavl and flatkv,
	// but all other traffic is written to memiavl only.
	//
	// CRITICAL: this is a test-only router and should never be deployed to production machines.
	TestOnlyDualWrite WriteMode = "test_only_dual_write"

	// Auto derives the effective write mode from the migration metadata
	// persisted in flatkv instead of fixing it by configuration, and
	// permits coordinated runtime transitions along the migration chain
	// via SetWriteMode (no restarts). flatkv is lazy under Auto: it is
	// created on disk by the first MigrateEVM transition, and its absence
	// means the store is effectively MemiavlOnly.
	//
	// Auto is only valid for stores whose history began in memiavl.
	// Switching an existing flatkv_only store to Auto is UNSUPPORTED and
	// breaks in one of two ways depending on the on-disk metadata: with
	// migration metadata present (e.g. a state-synced store), every
	// commit fails the cosmos/flatkv version-mismatch check; with
	// metadata absent (a genesis flatkv chain), mode derivation resolves
	// MemiavlOnly and reads silently route to an empty memiavl. Chains
	// that start in flatkv mode must stay configured flatkv_only.
	//
	// Note: per-key state proofs are only supported for memiavl-resident
	// data (flatkv has no proof builder). This is independent of Auto,
	// but becomes operator-visible once an Auto chain migrates stores to
	// flatkv.
	Auto WriteMode = "auto"
)

// IsValid returns true if the write mode is a recognized value
func (m WriteMode) IsValid() bool {
	switch m {
	case MemiavlOnly, MigrateEVM, EVMMigrated, MigrateAllButBank,
		AllMigratedButBank, MigrateBank, FlatKVOnly, TestOnlyDualWrite, Auto:
		return true
	default:
		return false
	}
}

// IsMigrationMode reports whether the mode is one of the active
// migration transitions (i.e. one that copies data from memiavl to
// flatkv in the background). Callers use it to decide when
// migration-specific setup is required, such as ensuring the
// MigrationStore tree exists on memiavl.
func (m WriteMode) IsMigrationMode() bool {
	switch m {
	case MigrateEVM, MigrateAllButBank, MigrateBank:
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

// migrationChain is the ordered sequence of write modes a chain walks
// through as data moves from memiavl to flatkv. The only legal runtime
// transition is one step forward along this chain.
var migrationChain = []WriteMode{
	MemiavlOnly,
	MigrateEVM,
	EVMMigrated,
	MigrateAllButBank,
	AllMigratedButBank,
	MigrateBank,
	FlatKVOnly,
}

// nextInChain returns the mode that follows m in migrationChain, or
// ("", false) if m is the end of the chain or not on it at all
// (Auto, TestOnlyDualWrite, unknown modes).
func nextInChain(m WriteMode) (WriteMode, bool) {
	for i, mode := range migrationChain {
		if mode == m && i+1 < len(migrationChain) {
			return migrationChain[i+1], true
		}
	}
	return "", false
}

// ValidateTransition returns nil if from -> to is a legal runtime
// write-mode transition, or an error describing why it is not.
//
// Legal transitions walk the migration chain forward one step at a time:
//
//	MemiavlOnly -> MigrateEVM -> EVMMigrated -> MigrateAllButBank ->
//	AllMigratedButBank -> MigrateBank -> FlatKVOnly
//
// ValidateTransition checks structural legality only; it is pure and
// consults no disk state. Transitions that leave a migration mode (i.e.
// from.IsMigrationMode() is true) are additionally only safe once that
// migration has completed — verifying that against persisted migration
// metadata is the caller's responsibility.
//
// from == to is not handled here; callers treat it as a no-op without
// calling ValidateTransition.
func ValidateTransition(from WriteMode, to WriteMode) error {
	if to == Auto || to == TestOnlyDualWrite || !to.IsValid() {
		return fmt.Errorf("write mode %q is not a legal transition target", to)
	}
	next, ok := nextInChain(from)
	if !ok {
		return fmt.Errorf("illegal write mode transition %q -> %q: there are no legal transitions from %q",
			from, to, from)
	}
	if to != next {
		return fmt.Errorf("illegal write mode transition %q -> %q: the only legal transition from %q is to %q",
			from, to, from, next)
	}
	return nil
}
