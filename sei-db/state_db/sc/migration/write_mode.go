package migration

import "fmt"

// WriteMode is the migration-package-local enumeration of the migrate-and-steady-state write
// strategies that the migration router supports. The pure single-backend layouts (memiavl-only
// and flatkv-only) are reached by NOT calling BuildRouter at all and instead writing through the
// underlying store handles directly. Eventually will be merged into config.WriteMode.
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
)

// IsValid returns true if the migration write mode is a recognized value.
func (m WriteMode) IsValid() bool {
	switch m {
	case MemiavlOnly, MigrateEVM, EVMMigrated,
		MigrateAllButBank, AllMigratedButBank, MigrateBank, FlatKVOnly, TestOnlyDualWrite:
		return true
	default:
		return false
	}
}

// ParseWriteMode converts a string to a migration WriteMode, returning an error if invalid.
func ParseWriteMode(s string) (WriteMode, error) {
	m := WriteMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("invalid migration write mode: %s", s)
	}
	return m, nil
}
