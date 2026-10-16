package migration

const (
	// The version of the migration when all data is in memiavl (i.e. we start from here)
	Version0_MemiavlOnly = 0
	// The version where EVM data lives in flatkv and all other data lives in memiavl.
	Version1_MigrateEVM = 1
	// Version 2 must stay unassigned: v6.7 persists it for a store whose bank data is still in
	// memiavl, so such a store has to fail to open as an unknown version.

	// The version where all data lives in flatkv.
	Version3_FlatKVOnly = 3
)
