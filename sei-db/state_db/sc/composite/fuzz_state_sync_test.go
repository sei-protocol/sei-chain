package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
)

// TestCompositeFuzzStateSyncAllModes drives a randomized state-sync
// round-trip for every config.WriteMode:
//
//  1. populate a source CompositeCommitStore with N blocks of workload;
//  2. open an exporter at the source's current version, drain it;
//  3. open a fresh destination store in the same mode, replay the export
//     stream through its importer, and reload it at the source's version;
//  4. verify the destination's Get / Has agree with the oracle for every
//     live key (the snapshot transferred a faithful copy);
//  5. resume the same workload schedule against the destination for an
//     additional M blocks and verify the destination keeps converging
//     with the oracle;
//  6. run end-of-test deep inspection of the destination's nested
//     backends.
//
// Active-migration modes drive enough source-side blocks to finish
// migration before exporting, so the destination receives a post-migration
// snapshot. This isolates the round-trip from any mid-migration resume
// behavior, which is exercised separately by
// TestCompositeFuzzStateSyncDuringMigration.
func TestCompositeFuzzStateSyncAllModes(t *testing.T) {
	const (
		srcBlocks = 100
		dstBlocks = 30
	)

	for _, profile := range allModeProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			rng := testutil.NewTestRandom()

			// ---- Source ----
			srcDir := t.TempDir()
			src := newCompositeForMode(t, t.Context(), srcDir, profile)

			oracle := newOracleStore()
			keysInUse := newLiveKeySet()
			simulateBlocksOnComposite(t, src, oracle, keysInUse, profile, rng,
				defaultWorkloadOpts(srcBlocks))

			require.Equal(t, int64(srcBlocks), src.Version(),
				"%s: pre-export src.Version must equal blocks driven", profile.name)

			exporter, err := src.Exporter(int64(srcBlocks))
			require.NoError(t, err, "%s: src.Exporter", profile.name)
			items := fuzzDrainExporter(t, exporter)
			require.NoError(t, exporter.Close())
			require.NoError(t, src.Close(), "%s: src.Close", profile.name)

			// ---- Destination ----
			dstDir := t.TempDir()
			dst := newCompositeForMode(t, t.Context(), dstDir, profile)
			// Close prior to opening the importer: existing
			// store_test.go tests do the same — the importer claims
			// ownership of the underlying backends, so the writable
			// handle must be released first.
			require.NoError(t, dst.Close(), "%s: dst pre-import Close", profile.name)

			importer, err := dst.Importer(int64(srcBlocks))
			require.NoError(t, err, "%s: dst.Importer", profile.name)
			fuzzReplayImport(t, importer, items)
			require.NoError(t, importer.Close(), "%s: importer.Close", profile.name)

			_, err = dst.LoadVersion(int64(srcBlocks), false)
			require.NoError(t, err, "%s: dst.LoadVersion at imported version", profile.name)
			defer func() { _ = dst.Close() }()

			require.Equal(t, int64(srcBlocks), dst.Version(),
				"%s: dst.Version after import must equal source version", profile.name)
			latest, err := dst.GetLatestVersion()
			require.NoError(t, err)
			require.Equal(t, int64(srcBlocks), latest,
				"%s: dst.GetLatestVersion after import must equal source version", profile.name)

			verifyReadsEqual(t, dst, oracle)

			// ---- Resume workload on destination ----
			opts := defaultWorkloadOpts(dstBlocks)
			opts.startingBlock = srcBlocks + 1
			simulateBlocksOnComposite(t, dst, oracle, keysInUse, profile, rng, opts)

			require.Equal(t, int64(srcBlocks+dstBlocks), dst.Version(),
				"%s: post-resume dst.Version mismatch", profile.name)

			verifyReadsEqual(t, dst, oracle)
			deepInspectPlacement(t, dst, oracle, profile)
		})
	}
}
