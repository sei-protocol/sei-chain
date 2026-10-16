package migration

const (
	// The version of the migration when all data is in memiavl (i.e. we start from here)
	Version0_MemiavlOnly = 0
	// The version where EVM data lives in flatkv and all other data lives in memiavl.
	Version1_MigrateEVM = 1
	// The version where all data lives in flatkv.
	Version2_FlatKVOnly = 2
)
