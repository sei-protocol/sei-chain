package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
)

// TestCompositeFuzzAllModes drives a randomized CRUD workload against
// CompositeCommitStore for every config.WriteMode, verifying:
//
//   - per-block oracle equivalence on Get / Has (and Iterator / GetProof
//     on capability-supporting stores);
//   - cs.Commit returns the expected monotonic version, and
//     cs.LastCommitInfo / cs.GetLatestVersion agree;
//   - end-of-test oracle equivalence across every live key;
//   - end-of-test deep inspection of the nested memiavl + flatkv backends
//     (every key is placed on the backend the mode dictates, no phantom
//     rows on either side, migration metadata in the expected state).
//
// Each mode runs in its own t.Run sub-test so a failure surfaces with the
// mode name attached. The same seed is reused across modes; print the
// seed at the top of the parent test for reproducibility.
func TestCompositeFuzzAllModes(t *testing.T) {
	const blocks = 100

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()

			cs := newCompositeForMode(t, t.Context(), t.TempDir(), profile)
			defer func() { _ = cs.Close() }()

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()

			simulateBlocksOnComposite(t, cs, oracle, keysInUse, profile, rng,
				defaultWorkloadOpts(blocks))

			require.Equal(t, int64(blocks), cs.Version(),
				"%s: cs.Version must equal blocks after the workload", profile.name)
			latest, err := cs.GetLatestVersion()
			require.NoError(t, err, "%s: GetLatestVersion", profile.name)
			require.Equal(t, int64(blocks), latest,
				"%s: GetLatestVersion must equal cs.Version", profile.name)

			verifyReadsEqual(t, cs, oracle)
			deepInspectPlacement(t, cs, oracle, profile)
		})
	}
}
