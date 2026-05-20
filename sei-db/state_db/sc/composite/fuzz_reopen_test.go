package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
)

// TestCompositeFuzzReopenAllModes exercises the close + reopen path of
// the CompositeCommitStore across every config.WriteMode. Each mode runs
// a randomized workload spanning multiple close/reopen cycles against the
// same on-disk directory, verifying:
//
//   - per-block oracle equivalence is maintained across reopen cycles;
//   - cs.GetLatestVersion immediately after each reopen agrees with the
//     last pre-close cs.Version;
//   - state surviving the reopen matches the oracle on Get / Has;
//   - end-of-test deep inspection of the nested backends after the final
//     reopen (no phantom rows on either side, every key on the backend
//     the mode dictates, migration metadata in the expected state).
//
// Active-migration modes have enough total workload across the cycles to
// drive migration to completion before the final deep inspection.
func TestCompositeFuzzReopenAllModes(t *testing.T) {
	const (
		blocksPerCycle = 30
		reopenCycles   = 3
	)

	for _, profile := range allModeProfiles() {
		profile := profile
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()
			dir := t.TempDir()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			var cs *CompositeCommitStore
			for cycle := 0; cycle < reopenCycles; cycle++ {
				cs = newCompositeForMode(t, t.Context(), dir, profile)

				// After the first cycle the just-loaded store must
				// already be at the last pre-close committed version.
				expectedStart := int64(cycle * blocksPerCycle)
				require.Equal(t, expectedStart, cs.Version(),
					"%s cycle=%d: cs.Version on reopen must equal pre-close version",
					profile.name, cycle)
				latest, err := cs.GetLatestVersion()
				require.NoError(t, err)
				require.Equal(t, expectedStart, latest,
					"%s cycle=%d: GetLatestVersion on reopen must equal cs.Version",
					profile.name, cycle)

				// Drive blocksPerCycle blocks of workload, numbering
				// them so simulateBlocksOnComposite asserts on
				// monotonically-increasing versions across all cycles.
				opts := defaultWorkloadOpts(blocksPerCycle)
				opts.startingBlock = int(expectedStart) + 1
				simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng, opts)

				// Sanity: post-cycle version is what we expect.
				expectedEnd := int64((cycle + 1) * blocksPerCycle)
				require.Equal(t, expectedEnd, cs.Version(),
					"%s cycle=%d: cs.Version after cycle must equal expected end",
					profile.name, cycle)

				// Oracle equivalence survives a Close-Reopen if we
				// run the check before close. We do; deep inspection
				// is reserved for after the final reopen.
				verifyReadsEqual(t, cs, oracle)

				require.NoError(t, cs.Close(),
					"%s cycle=%d: Close must not error", profile.name, cycle)
			}

			// Final reopen for end-of-test inspection. This is the
			// "post-restart steady state" the deep inspection cares
			// about: every backend is opened from disk, no in-memory
			// state survives.
			cs = newCompositeForMode(t, t.Context(), dir, profile)
			defer func() { _ = cs.Close() }()

			finalVersion := int64(reopenCycles * blocksPerCycle)
			require.Equal(t, finalVersion, cs.Version(),
				"%s: cs.Version after final reopen must equal cumulative workload size",
				profile.name)

			verifyReadsEqual(t, cs, oracle)
			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}
