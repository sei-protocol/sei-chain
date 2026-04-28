package config

import "fmt"

// WriteMode defines how EVM data writes are routed between backends.
type WriteMode string

const (
	// MemIAVLOnly writes all data to memiavl only. This is the default/legacy behavior.
	//
	// Migration version 0.
	MemIAVLOnly WriteMode = "memiavl_only"

	// DualWrite writes EVM data to both Cosmos and EVM backends. For testing only, do not use in production.
	DualWrite WriteMode = "dual_write"

	// Migreate the evm/ module from memiavl to flatkv.
	//
	// Handles the transition from migration version 0 to 1,
	// and continues to function once we reach migration version 1.
	MigrateEVM WriteMode = "migrate_evm"

	// After the evm/ module has been migrated, but before we are ready to do the next migraiton.
	//
	// Migration version 1.
	EVMMigrated WriteMode = "evm_migrated"

	// Migrate the all but the bank module from memiavl to flatkv.
	//
	// Handles the transition from migration version 1 to 2,
	// and continues to function once we reach migration version 2.
	MigrateAllButBank WriteMode = "migrate_all_but_bank"

	// After the all but the bank module has been migrated, but before we are ready to do the next migraiton.
	//
	// Migration version 2.
	AllMigratedButBank WriteMode = "all_migrated_but_bank"

	// Migrate the bank module from memiavl to flatkv.
	//
	// Handles the transition from migration version 2 to 3,
	// and continues to function once we reach migration version 3.
	MigrateBank WriteMode = "migrate_bank"

	// FlatKVOnly writes all data to flatkv only. To be used after all data is migrated out of memiavl.
	//
	// Migration version 3.
	FlatKVOnly WriteMode = "flatkv_only"
)

// IsValid returns true if the write mode is a recognized value
func (m WriteMode) IsValid() bool {
	switch m {
	case MemIAVLOnly, DualWrite, MigrateEVM, EVMMigrated,
		MigrateAllButBank, AllMigratedButBank, MigrateBank, FlatKVOnly:
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
