package operations

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/spf13/cobra"
)

// MigrateEvmStatusCmd is the seidb subcommand that reports the on-disk
// 0->1 (MigrateEVM) migration state of a FlatKV directory.
//
// It exists so that the cluster-level integration test driver can poll
// "is migration done yet?" against each validator's data dir from the
// host without having to add a custom RPC handler or grep through node
// logs. JSON output is the contract; the test runner shells out to this
// tool, parses the result, and decides when to advance.
//
// Concretely it reads two reserved keys from the FlatKV migration
// store:
//
//   - migration-version: an 8-byte big-endian uint64 written exactly
//     once per migration lifecycle on the bump block. Absent or zero
//     means MigrateEVM has not yet completed.
//   - migration-boundary: the in-flight cursor encoding the
//     (module, key) pair the next batch should resume from. Present iff
//     the boundary is somewhere strictly between not-started and
//     complete.
//
// To stay aligned with the rest of the seidb tools the read goes
// through openFlatKVReadOnly, which hardlink-clones the latest snapshot
// + copies the WAL into a temp dir before opening. That avoids
// contending with a live node for the FlatKV writer lock and gives the
// tool a stable view even if the live writer rolls snapshots mid-run.
func MigrateEvmStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-evm-status",
		Short: "Report on-disk MigrateEVM (0->1) migration status as JSON",
		Long: "Reads the migration-version and migration-boundary keys " +
			"from a FlatKV store and prints a JSON summary of the 0->1 " +
			"migration state. Intended for use by integration test drivers " +
			"polling for migration completion from the host.",
		Run: executeMigrateEvmStatus,
	}
	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV database directory")
	cmd.PersistentFlags().Int64("height", 0, "FlatKV target version; 0 selects the latest available version")
	return cmd
}

func executeMigrateEvmStatus(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")

	if dbDir == "" {
		panic("Must provide --db-dir pointing at a FlatKV data directory")
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		panic(fmt.Errorf("open flatkv read-only: %w", err))
	}
	defer func() { _ = store.Close() }()

	versionAt := store.Version()

	// Direct flatkv.Get is the same reader path the migration manager
	// itself uses; the version and boundary keys are stored verbatim
	// under the "migration" module.
	migrationVersion := uint64(migration.Version0_MemiavlOnly)
	versionRaw, hasVersion := store.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
	if hasVersion {
		// 8-byte big-endian, written by migration_manager.go on the
		// bump block. A short value here is a corruption signal, not
		// a half-written entry — flatkv commits atomically — but we
		// still tolerate it so the JSON output stays informative.
		if len(versionRaw) == 8 {
			migrationVersion = uint64(versionRaw[0])<<56 |
				uint64(versionRaw[1])<<48 |
				uint64(versionRaw[2])<<40 |
				uint64(versionRaw[3])<<32 |
				uint64(versionRaw[4])<<24 |
				uint64(versionRaw[5])<<16 |
				uint64(versionRaw[6])<<8 |
				uint64(versionRaw[7])
		}
	}

	boundaryRaw, hasBoundary := store.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))

	out := struct {
		VersionAt          int64  `json:"version_at"`
		MigrationVersion   uint64 `json:"migration_version"`
		MigrateEVMComplete bool   `json:"migrate_evm_complete"`
		BoundaryPresent    bool   `json:"boundary_present"`
		BoundaryHex        string `json:"boundary_hex,omitempty"`
		VersionRawHex      string `json:"version_raw_hex,omitempty"`
	}{
		VersionAt:          versionAt,
		MigrationVersion:   migrationVersion,
		MigrateEVMComplete: migrationVersion >= uint64(migration.Version1_MigrateEVM),
		BoundaryPresent:    hasBoundary,
	}
	if hasBoundary {
		out.BoundaryHex = hex.EncodeToString(boundaryRaw)
	}
	if hasVersion {
		out.VersionRawHex = hex.EncodeToString(versionRaw)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		panic(fmt.Errorf("encode json: %w", err))
	}
}
