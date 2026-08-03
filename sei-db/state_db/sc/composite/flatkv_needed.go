package composite

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
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
// erroring — fabricating a nonexistence answer. Every uncertain case therefore resolves toward "yes" or toward
// an error, never toward a silent "no".
//
// The answer is derived from two records flatkv persists, read through a throwaway read-only view opened at
// flatkv's tip:
//
//   - the effective layout derived from the migration metadata, which distinguishes "the directory exists but
//     no migration ever started" from a chain where flatkv participates;
//   - flatkv's earliest-history record, written by the seeding SetInitialVersion, which marks where its
//     history begins.
//
// It keys on the earliest-history record rather than on the flatkv open failing, because "no snapshot at
// target" is also what a pruned or corrupt in-history height produces. Serving those from post-migration
// memiavl is the fabricated-nonexistence case above, so they must keep failing loudly.
//
// The view is opened at the tip rather than at height because both records are properties of the store as a
// whole, not of any one height. Opening it costs a directory clone and five Pebble opens; callers on
// latency-sensitive paths should note that this runs per call, by design — the alternative is reading flatkv's
// in-memory copy of these records, which is populated only as a side effect of a load and is therefore silently
// stale on any handle that has not been loaded.
//
// A nil fkv means the backend was never materialized: under types.Auto the constructor only builds flatkv when
// its directory already exists on disk, so absence is itself the answer. A non-Auto configured mode short
// circuits to true, preserving the pinned fail-loud behavior of fixed modes, which cannot re-derive an
// effective memiavl-only layout.
func FlatKVNeededAtHeight(fkv flatkv.Store, configuredMode types.WriteMode, height int64) (bool, error) {
	if fkv == nil {
		return false, nil
	}
	if configuredMode != types.Auto {
		return true, nil
	}
	if height <= 0 {
		// The latest height is always in-era when flatkv exists at all.
		return true, nil
	}

	view, err := fkv.LoadVersionReadOnly(0)
	if err != nil {
		// Deliberately an error, not a false. A chain whose flatkv directory exists but whose tip cannot be
		// opened is either mid-materialization from a crashed transition or corrupt, and those are not
		// distinguishable here. Reporting "not needed" would serve the height from memiavl with the migrated
		// keys already deleted, answering "absent" for keys that exist; a running node reaches the same
		// conclusion loudly, by failing its own load.
		return false, fmt.Errorf("failed to open flatkv tip to classify height %d: %w", height, err)
	}
	defer func() {
		if closeErr := view.Close(); closeErr != nil {
			logger.Error("failed to close flatkv classification view", "err", closeErr)
		}
	}()

	derived, err := migration.DeriveWriteMode(view)
	if err != nil {
		return false, fmt.Errorf("failed to derive write mode while classifying height %d: %w", height, err)
	}
	if derived == types.MemiavlOnly {
		logger.Info("flatkv directory exists but no migration has started; height is memiavl-only",
			"height", height)
		return false, nil
	}

	earliest := view.EarliestVersion()
	if earliest > 0 && height < earliest {
		logger.Info("height predates flatkv history; memiavl serves it alone",
			"height", height, "flatkvEarliestVersion", earliest)
		return false, nil
	}
	return true, nil
}
