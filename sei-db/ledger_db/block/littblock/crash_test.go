package littblock_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

const (
	// crashCohorts and crashPerCohort size the fixture both crash tests write.
	crashCohorts    = 4
	crashPerCohort  = 5
	crashTotalBlock = crashCohorts * crashPerCohort
)

// TestNoBlockWithoutQCAfterTornTail is the end-to-end proof of the headline crash
// invariant: after a torn write, every surviving block is still covered by a
// surviving QC. A surviving QC may be missing some of its blocks; never the
// reverse.
//
// It writes QC-then-blocks cohorts into the single-shard ledger table, then
// physically truncates the tail of the segment's value file — dropping the
// last-written bytes — and marks the segment unsealed so reopening runs litt's
// group-atomic recovery, which keeps a contiguous write-order prefix. Because
// every covering QC is written before its blocks, that prefix can never contain a
// block whose QC was dropped.
//
// The segment-level TestSealLoadedSegmentSingleShardPrefix proves the underlying
// single-shard prefix property in isolation; this pins the store behavior that
// depends on it: one shared table, QC before block.
func TestNoBlockWithoutQCAfterTornTail(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, littConfig(t, dir))
	blocktest.WriteCohorts(t, db, crashCohorts, crashPerCohort)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Corrupt the segment holding the most value bytes (where the most recent
	// writes landed): drop the tail of its value file, then flip its metadata back
	// to unsealed so LoadSegment re-runs recovery on reopen.
	//
	// Dropping tornTailBytes models a partial write of the last value. That stays
	// a single torn tail, rather than spilling across value boundaries, only while
	// every stored value is larger than it.
	const tornTailBytes = 16
	require.Greater(t, len(blocktest.RecordValue(blocktypes.KindBlock, 0)), tornTailBytes,
		"fixture values must exceed the torn tail, or the truncation model breaks")

	valPath, metaPath := largestValueSegmentFiles(t, dir)
	truncateFileBy(t, valPath, tornTailBytes)
	markSegmentUnsealedOnDisk(t, metaPath)

	// Reopen: recovery discards the torn tail, keeping a contiguous prefix.
	db2 := openLitt(t, littConfig(t, dir))
	defer func() { _ = db2.Close() }()

	survived := requireEveryBlockHasItsQC(t, db2)

	// The truncation must have actually dropped a block, otherwise the recovery
	// path was never exercised and the invariant proves nothing.
	require.Less(t, survived, crashTotalBlock, "expected the torn tail to drop at least one block")
}

// TestFlushSurvivesHardKill is the counterpart to TestNoBlockWithoutQCAfterTornTail:
// where the torn-tail test proves an unflushed partial write degrades to a
// contiguous prefix, with some loss expected, this proves a clean Flush loses
// NOTHING across a real, uncatchable process kill.
//
// It re-execs this test binary as a child (gated by crashChildEnv) that writes
// every cohort, Flushes, then SIGKILLs itself. SIGKILL cannot be caught, so no
// deferred Close and no graceful shutdown run — it is the strongest possible
// "kill process". Because the kill happens only after Flush returns, the parent
// can reopen the DB the dead child left behind and require every flushed record
// to be present. We must not SIGKILL the process running the tests, so the crash
// is isolated to the child subprocess.
func TestFlushSurvivesHardKill(t *testing.T) {
	if os.Getenv(crashChildEnv) == "1" {
		// Child branch: write, flush, then crash. Never returns.
		runCrashChild(t)
		return
	}

	if runtime.GOOS == "windows" {
		t.Skip("hard-kill crash test relies on Unix SIGKILL / WaitStatus")
	}

	// t.TempDir is removed when this test ends, cleaning up the child's data too.
	dir := t.TempDir()

	// Re-exec only this test as the crash child, pointed at dir. We pass just
	// -test.run and -test.v (not the parent's flags) so the child writes no
	// coverprofile it can never finish.
	cmd := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v") //nolint:gosec // os.Args[0] is the test binary
	cmd.Env = append(os.Environ(), crashChildEnv+"=1", crashDirEnv+"="+dir)
	out, err := cmd.CombinedOutput()

	// The child MUST have died from SIGKILL; otherwise no real crash happened (it
	// exited cleanly or failed a require) and the test proves nothing.
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "child should have been signal-killed; output:\n%s", out)
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok, "unexpected wait status type %T; output:\n%s", exitErr.Sys(), out)
	require.True(t, ws.Signaled(), "child should exit via signal; output:\n%s", out)
	require.Equal(t, syscall.SIGKILL, ws.Signal(), "child should be SIGKILLed; output:\n%s", out)

	// Reopen the DB the crashed child left behind and assert nothing flushed was lost.
	db := openLitt(t, littConfig(t, dir))
	defer func() { _ = db.Close() }()

	survived := requireEveryBlockHasItsQC(t, db)

	// Unlike the torn-tail test, which expects loss, a clean Flush before the kill
	// must lose nothing.
	require.Equal(t, crashTotalBlock, survived, "flushed blocks must all survive a hard kill")
}

// runCrashChild is the subprocess body of TestFlushSurvivesHardKill: it opens the
// DB at crashDirEnv, writes every cohort, Flushes, then hard-kills its own
// process. It never returns.
func runCrashChild(t *testing.T) {
	dir := os.Getenv(crashDirEnv)
	require.NotEmpty(t, dir)

	db := openLitt(t, littConfig(t, dir))
	blocktest.WriteCohorts(t, db, crashCohorts, crashPerCohort)
	require.NoError(t, db.Flush())

	// Hard crash: uncatchable, runs no defers and no graceful Close.
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGKILL))
	select {} // unreachable — block so nothing else runs before the kernel reaps us
}

// requireEveryBlockHasItsQC fails unless every surviving block record is still
// covered by a surviving QC, and returns how many blocks survived.
//
// Survivors are enumerated by scanning rather than by reading each number in
// turn: the scan reports what recovery kept, where a point read of a number
// whose value was torn away fails instead of reporting the record as absent.
func requireEveryBlockHasItsQC(t *testing.T, db blocktypes.BlockDB) int {
	t.Helper()
	it, err := db.Scan(false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var blocks []uint64
	qcs := map[uint64]bool{}
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		switch it.Kind() {
		case blocktypes.KindBlock:
			blocks = append(blocks, it.Number())
		case blocktypes.KindQC:
			qcs[it.Number()] = true
		}
	}

	for _, n := range blocks {
		cohortStart := n - n%crashPerCohort
		require.True(t, qcs[cohortStart],
			"block %d survived but its covering QC at %d was lost", n, cohortStart)
	}
	return len(blocks)
}

// largestValueSegmentFiles walks the litt data directory under dir and returns
// the value-file and sibling metadata-file paths of the segment with the most
// value bytes (the one most recently written into; robust to empty rollover
// segments that may exist after a clean Close).
func largestValueSegmentFiles(t *testing.T, dir string) (valPath string, metaPath string) {
	t.Helper()
	var bestSize int64 = -1
	var bestIndex string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), segment.ValuesFileExtension) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > bestSize {
			// File name is "<index>-<shard>.values"; the index is everything
			// before the dash.
			base := strings.TrimSuffix(d.Name(), segment.ValuesFileExtension)
			index := base
			if i := strings.IndexByte(base, '-'); i >= 0 {
				index = base[:i]
			}
			bestSize = info.Size()
			bestIndex = index
			valPath = p
		}
		return nil
	}))
	require.NotEmpty(t, valPath, "no value file found under %s", dir)
	_, err := strconv.ParseUint(bestIndex, 10, 32)
	require.NoError(t, err, "unexpected segment index %q", bestIndex)
	metaPath = filepath.Join(filepath.Dir(valPath), bestIndex+segment.MetadataFileExtension)
	return valPath, metaPath
}

// truncateFileBy drops the last n bytes of the file at p.
func truncateFileBy(t *testing.T, p string, n int) {
	t.Helper()
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Greater(t, len(data), n, "file %s too small to truncate by %d", p, n)
	require.NoError(t, os.WriteFile(p, data[:len(data)-n], 0600))
}

// markSegmentUnsealedOnDisk flips the sealed byte in a segment's metadata file
// from 1 back to 0, simulating a segment that crashed before sealing so that
// LoadSegment runs the recovery path on reopen.
func markSegmentUnsealedOnDisk(t *testing.T, metaPath string) {
	t.Helper()
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, segment.V4MetadataSize, len(data), "unexpected metadata size for %s", metaPath)
	data[segment.MetadataSealedByteOffset] = 0
	require.NoError(t, os.WriteFile(metaPath, data, 0600))
}

const (
	// crashChildEnv gates the child branch of TestFlushSurvivesHardKill. When set,
	// the test re-runs as the crash subprocess instead of the parent orchestrator.
	crashChildEnv = "LITTBLOCK_CRASH_CHILD"
	// crashDirEnv carries the data directory the parent created down to the child,
	// so both processes operate on the same on-disk DB.
	crashDirEnv = "LITTBLOCK_CRASH_DIR"
)
