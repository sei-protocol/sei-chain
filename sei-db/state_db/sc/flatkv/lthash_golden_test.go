package flatkv

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// This file pins flatKV's lattice hashes to values recorded on disk, so that a change to how hashing is
// organised has to either reproduce them or be seen changing them.
//
// The rest of the lthash suite checks hashes against things derived at the same time as the hashes: a
// full rescan, or a model built from the same changeset stream. Those catch a wrong answer, but not an
// answer that changed. This does, because the expected values were computed by a build that no longer
// exists and are read back from testdata rather than recomputed.
//
// The recorded archive is produced by the same code path that reports hashes in production — the
// finalization goroutine reporting into a hashlog.HashLogger the store was built with — so the format
// is a CSV of one row per block, and comparing two runs is hashlog.CompareHashesInRange.

// goldenRecord regenerates the committed archive instead of checking against it. Off by default, and
// refused outright on CI: see recordGoldenArchive.
var goldenRecord = flag.Bool("lthash-golden-record", false,
	"rewrite the committed lthash golden archive from this build instead of verifying against it")

const (
	// goldenSeed drives the workload. A literal rather than lthash_agreement_test.go's agreementSeed,
	// whose -lthash-agreement-seed flag would silently change what the recorded hashes describe.
	goldenSeed = 0x5ea1_0000_600d_1eaf

	// goldenBlocks is how many blocks the workload produces. Enough that a block's hash depends on a long
	// chain of predecessors, so an accumulator that loses a delta diverges and stays diverged.
	goldenBlocks = 12

	// goldenOpsPerBlock is each block's operation budget, split by planBlock across creates, updates,
	// deletes and deletes of absent keys.
	goldenOpsPerBlock = 1000

	// goldenVersion is embedded in the archive's file names, so it is fixed rather than the real build
	// version: the recorded archive has to keep the name it was recorded under.
	goldenVersion = "lthash-golden"

	// goldenArchiveDir holds the recorded archive, committed to the repository.
	goldenArchiveDir = "testdata/lthash_golden"
)

// TestLtHashGoldenHashesUnchanged replays the recorded workload and requires this build to produce the
// hashes that are committed under testdata.
//
// A failure here is one of two things, and the changeset column says which. If the "changeset" hashes
// differ, the workload itself changed and the lattice hashes are incomparable — fix the generator. If
// only the flatKV columns differ, this build hashes the same blocks differently, which is the failure
// this test exists for.
func TestLtHashGoldenHashesUnchanged(t *testing.T) {
	if *goldenRecord {
		recordGoldenArchive(t)
		return
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	writeGoldenRun(t, fresh, config.DefaultTestConfig(t))
	requireArchivesAgree(t, goldenArchiveDir, fresh)
}

// TestLtHashGoldenIsIndependentOfWorkerCount requires the recorded hashes to hold under a different
// lthash worker count.
//
// MixIn and MixOut are commutative and associative, so the number of workers and the chunk size cannot
// move the result. That is the property the parallel fold rests on, and it is what will let hashing be
// reorganised without the hashes moving — so it is asserted rather than assumed.
func TestLtHashGoldenIsIndependentOfWorkerCount(t *testing.T) {
	for _, threadsPerCore := range []float64{0.5, 4.0} {
		t.Run(fmt.Sprintf("threadsPerCore=%v", threadsPerCore), func(t *testing.T) {
			cfg := config.DefaultTestConfig(t)
			cfg.LtHashThreadsPerCore = threadsPerCore

			fresh := filepath.Join(t.TempDir(), "fresh")
			writeGoldenRun(t, fresh, cfg)
			requireArchivesAgree(t, goldenArchiveDir, fresh)
		})
	}
}

// requireArchivesAgree requires the two archives to report identical hashes for every golden block.
//
// requireEveryBlock is what makes a pass mean something: without it an archive that recorded nothing —
// because the run died early, or the directory was wrong — compares equal to anything.
func requireArchivesAgree(t *testing.T, recorded string, fresh string) {
	t.Helper()

	diffs, err := hashlog.CompareHashesInRange(recorded, fresh, 1, goldenBlocks, -1, true)
	require.NoError(t, err, "comparing recorded archive %s against this build's %s", recorded, fresh)

	for _, diff := range diffs {
		t.Errorf("block %s hashes differ:\n  recorded: %s\n  this build: %s",
			diffBlockLabel(diff), formatReports(diff.HashesFromA), formatReports(diff.HashesFromB))
	}
	require.Empty(t, diffs, "this build does not reproduce the recorded lattice hashes; "+
		"if the change is intended, re-record with -lthash-golden-record and review the diff")
}

// writeGoldenRun drives the golden workload against a fresh store and records each block's hashes into
// an archive at dir.
func writeGoldenRun(t *testing.T, dir string, cfg *config.Config) {
	t.Helper()

	logger := newGoldenHashLogger(t, dir, hashCategories())
	defer func() { require.NoError(t, logger.Close()) }()

	store := setupTestStoreWithHashLogger(t, cfg, logger)
	defer func() { require.NoError(t, store.Close()) }()

	workload := newFixedSizeAgreementWorkload(
		rand.New(rand.NewSource(goldenSeed)), //nolint:gosec // deterministic test data only
		goldenOpsPerBlock)

	for height := int64(1); height <= goldenBlocks; height++ {
		changeSets := workload.nextBlock(height)
		require.NotEmpty(t, changeSets, "block %d produced no changesets", height)
		require.NoError(t, store.ApplyChangeSets(height, changeSets), "apply block %d", height)

		_, err := store.Commit(height)
		require.NoError(t, err, "commit block %d", height)
		require.Equal(t, height, store.Version())

		block := uint64(height) //nolint:gosec // heights start at 1 and only increase
		logger.ReportChangeset(block, changeSets)
	}

	// Hashes are reported off the commit path, so the run is not complete until they have caught up.
	require.NoError(t, store.FlushHashes())
}

// newGoldenHashLogger opens a logger that records the store's hash categories plus the changeset column
// into dir.
//
// The file size cap is raised well past what this workload writes, because a rotation would split the
// archive across files named after the blocks they hold, and the recorded names have to stay stable.
func newGoldenHashLogger(t *testing.T, dir string, hashTypes []string) hashlog.HashLogger {
	t.Helper()

	cfg := hashlog.DefaultHashLoggerConfig(dir, goldenVersion)
	cfg.HashTypes = hashTypes
	cfg.TargetFileSize = unit.MB

	logger, err := hashlog.NewHashLogger(cfg)
	require.NoError(t, err)
	return logger
}

// recordGoldenArchive replaces the committed archive with one produced by this build.
//
// It refuses to run on CI. The archive is the only statement of what the hashes are expected to be, so a
// build that regenerated it as part of an ordinary test run would report success for having agreed with
// itself. Recording is a deliberate local act whose output a human reads in a diff.
func recordGoldenArchive(t *testing.T) {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the lthash golden archive on CI: " +
			"the recorded hashes are the expected values, so a build that rewrites them verifies nothing")
	}

	require.NoError(t, os.RemoveAll(goldenArchiveDir))
	writeGoldenRun(t, goldenArchiveDir, config.DefaultTestConfig(t))

	t.Logf("recorded %d blocks into %s — review the diff before committing", goldenBlocks, goldenArchiveDir)
}

// diffBlockLabel names the block a diff describes, taking it from whichever side has a report.
func diffBlockLabel(diff *hashlog.HashLogPair) string {
	for _, reports := range [][]*hashlog.HashLog{diff.HashesFromA, diff.HashesFromB} {
		if len(reports) > 0 {
			return fmt.Sprintf("%d", reports[0].BlockNumber)
		}
	}
	return "(unknown)"
}

// formatReports renders one side of a diff for a failure message, hash types in a stable order since
// they come out of a map.
func formatReports(reports []*hashlog.HashLog) string {
	if len(reports) == 0 {
		return "(no report)"
	}
	var out string
	for _, report := range reports {
		types := make([]string, 0, len(report.Hashes))
		for hashType := range report.Hashes {
			types = append(types, hashType)
		}
		slices.Sort(types)
		for _, hashType := range types {
			out += fmt.Sprintf("\n    %-24s %x", hashType, report.Hashes[hashType])
		}
	}
	return out
}
