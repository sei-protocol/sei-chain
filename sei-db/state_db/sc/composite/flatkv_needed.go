package composite

import (
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// FlatKVNeededAtHeight reports whether flatkv holds any consensus state at height, i.e. whether a store
// serving reads at that height must open flatkv or can be served completely by memiavl alone.
//
// A chain's backend layout is not fixed for the life of the chain. Under types.Auto a chain runs memiavl-only
// until a MigrateEVM transition materializes flatkv and seeds it at some height; below that height every
// consensus value — including evm — lives in memiavl, and flatkv has no data at all. Block history therefore
// splits into a pre-flatkv era and an in-flatkv era, and a reader at an old height must know which side it is
// on. Answering "yes" for a pre-era height sends the caller into a flatkv open that cannot succeed ("no
// snapshot found ..."), failing historical queries that worked when the node was configured memiavl_only.
//
// Answering "no" when flatkv does hold state at that height is the far more dangerous direction: the migration
// deletes migrated keys out of memiavl, so a memiavl-only reader would report a key as absent rather than
// erroring — fabricating a nonexistence answer. Every uncertain case therefore resolves toward "yes", never
// toward a silent "no".
//
// The answer keys on flatkv's earliest-history record rather than on a flatkv open failing, because "no
// snapshot at target" is also what a pruned or corrupt in-history height produces. Serving those from
// post-migration memiavl is the fabricated-nonexistence case above, so they must keep failing loudly.
//
// This performs no I/O and cannot fail, which is what lets historical queries call it per read.
func FlatKVNeededAtHeight(
	// Whether a flatkv backend was materialized at all. Under types.Auto the constructor only builds
	// flatkv when its directory already exists on disk, so absence is itself the answer: no flatkv
	// instance means no height was ever served by one.
	flatKVPresent bool,
	// The height flatkv's history begins at, or 0 when that is unknown — history begins at genesis, or
	// seeding never ran. Both zero cases resolve toward "yes" per the safety direction above. Callers
	// pass CompositeCommitStore.flatKVEarliestVersion, which is read from disk once at construction;
	// reading flatkv's own copy instead is wrong, because it is populated as a side effect of a load and
	// so is zero on a handle that has never been loaded.
	earliestVersion int64,
	// The configured write mode, not the effective one. A fixed mode short circuits to "yes", preserving
	// the pinned fail-loud behavior of modes that cannot re-derive an effective memiavl-only layout.
	configuredMode types.WriteMode,
	// The height being read, where 0 means latest.
	height int64,
) bool {
	if !flatKVPresent {
		return false
	}
	if configuredMode != types.Auto {
		return true
	}
	if height <= 0 {
		// The latest height is always in-era when flatkv exists at all.
		return true
	}
	if earliestVersion > 0 && height < earliestVersion {
		logger.Info("height predates flatkv history; memiavl serves it alone",
			"height", height, "flatkvEarliestVersion", earliestVersion)
		return false
	}
	return true
}
