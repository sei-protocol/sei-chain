package migration

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// migrationTargetVersions maps each migration mode to the migration
// version its completion persists via MigrationVersionKey.
var migrationTargetVersions = map[types.WriteMode]uint64{
	types.MigrateEVM:        Version1_MigrateEVM,
	types.MigrateAllButBank: Version2_MigrateAllButBank,
	types.MigrateBank:       Version3_FlatKVOnly,
}

// IsModeComplete reports whether the given write mode has finished its
// work. Steady-state modes perform no migration, so they are always
// complete. A migration mode is complete once its version bump has been
// written to flatkv's MigrationStore (the MigrationVersionKey reaching
// the mode's target version). The version key is checked directly rather
// than via DeriveWriteMode: it is a monotonic completion signal, whereas
// interpreting the derived mode would require comparing positions along
// the migration chain.
//
// Durability caveat: on a live store, flatkv reads see pending
// (uncommitted) writes, so the reported completion may not yet be
// durable. Callers gating consensus-relevant decisions (SetWriteMode)
// must invoke this between blocks, after the completing block's Commit —
// at that point the read reflects committed state. Read-only handles
// report committed state as-of their loaded version and carry no such
// caveat.
func IsModeComplete(flatKV flatkv.Store, mode types.WriteMode) (bool, error) {
	if !mode.IsMigrationMode() {
		return true, nil
	}
	if flatKV == nil {
		return false, fmt.Errorf("cannot check completion of mode %q: flatkv store is nil", mode)
	}
	version, _, err := readVersionFromDB(buildFlatKVReader(flatKV))
	if err != nil {
		return false, fmt.Errorf("failed to read migration version: %w", err)
	}
	return version >= migrationTargetVersions[mode], nil
}

// DeriveWriteMode derives the effective write mode from the migration
// metadata persisted in flatkv's MigrationStore (MigrationVersionKey and
// MigrationBoundaryKey). Used by the composite store when configured with
// types.Auto.
//
// An absent MigrationVersionKey is interpreted as migration version 0. A
// migration is considered in flight when a persisted boundary has a status
// other than MigrationNotStarted: a boundary record that has not yet
// advanced past NotStarted is indistinguishable from an unstarted
// migration (mirroring the lattice-append gate in the composite store).
func DeriveWriteMode(flatKV flatkv.Store) (types.WriteMode, error) {
	if flatKV == nil {
		return "", fmt.Errorf("cannot derive write mode: flatkv store is nil")
	}
	reader := buildFlatKVReader(flatKV)

	version, _, err := readVersionFromDB(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read migration version: %w", err)
	}
	boundary, err := readMigrationBoundary(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read migration boundary: %w", err)
	}
	inFlight := boundary.Status() != MigrationNotStarted

	switch version {
	case Version0_MemiavlOnly:
		if inFlight {
			return types.MigrateEVM, nil
		}
		return types.MemiavlOnly, nil
	case Version1_MigrateEVM:
		if inFlight {
			return types.MigrateAllButBank, nil
		}
		return types.EVMMigrated, nil
	case Version2_MigrateAllButBank:
		if inFlight {
			return types.MigrateBank, nil
		}
		return types.AllMigratedButBank, nil
	case Version3_FlatKVOnly:
		if inFlight {
			// The completion block deletes the boundary atomically with the
			// final version bump, so a boundary at version 3 is corrupt state.
			return "", fmt.Errorf(
				"corrupt migration metadata: migration version %d with an in-flight boundary %q",
				version, boundary.String())
		}
		return types.FlatKVOnly, nil
	default:
		return "", fmt.Errorf("unknown migration version %d", version)
	}
}
