// Command staticmigrate performs an offline, static migration of a memIAVL
// file tree into a new memIAVL instance and a flatKV file tree.
//
// It reads every key/value pair out of the source memIAVL snapshot as fast as
// possible by scanning each module's leaf records directly (sequential I/O, no
// tree traversal) across N reader goroutines, and feeds them through a large
// buffered channel to a single consumer that "handles" each pair (writing into
// the destination memIAVL and flatKV stores). The handler body is left as a
// TODO.
//
// The destination stores acquire file locks, so any node using these
// directories must be stopped while this tool runs.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

const (
	// numReaders is the number of parallel reader goroutines per module.
	// Hard-coded for now; tune toward NVMe queue depth / core count later.
	numReaders = 8

	// channelCapacity is the buffer size of the channel between the reader
	// goroutines and the single consumer. Large by design so readers rarely
	// block on a slow consumer.
	channelCapacity = 1 << 16

	// progressInterval controls how often progress is logged (time-based, not
	// count-based).
	progressInterval = 10 * time.Second
)

// kvPair is a single key/value record handed from a reader to the consumer.
type kvPair struct {
	key   []byte
	value []byte
}

func main() {
	// cobra prints the error itself; we just set the exit code.
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var height int64

	cmd := &cobra.Command{
		Use:   "staticmigrate <input-memiavl> <out-memiavl> <out-flatkv>",
		Short: "Statically migrate a memIAVL file tree into a new memIAVL instance and a flatKV file tree",
		Long: `Statically migrate a memIAVL file tree.

Reads the source memIAVL snapshot module-by-module, scanning each module's
leaf records directly across multiple goroutines, and feeds every key/value
pair through a single handler that writes into a new memIAVL instance and a
flatKV file tree.

Arguments (all required, positional):
  input-memiavl   source memIAVL directory: either a full memIAVL tree
                  (e.g. .../state_commit/memiavl, with a 'current' symlink or
                  snapshot-<version> subdirectories) or an already-extracted
                  snapshot directory itself
  out-memiavl     destination memIAVL directory
  out-flatkv      destination flatKV directory

The source is read at a snapshot boundary (the 'current' snapshot by default,
or the snapshot for --height). Any un-snapshotted changelog (WAL) tail is not
included.

The destination stores take file locks; any node using these directories must
be stopped while this tool runs.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				return fmt.Errorf(
					"expected 3 positional arguments but received %d\n\n"+
						"  usage: %s\n\n"+
						"  input-memiavl   source memIAVL directory (e.g. .../state_commit/memiavl)\n"+
						"  out-memiavl     destination memIAVL directory (created if missing)\n"+
						"  out-flatkv      destination flatKV directory (created if missing)",
					len(args), cmd.UseLine())
			}
			return nil
		},
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return run(args[0], args[1], args[2], height)
		},
	}

	cmd.Flags().Int64Var(&height, "height", 0, "source snapshot version (0 = latest/current snapshot)")

	return cmd
}

func run(inputDir, outMemiavlDir, outFlatkvDir string, height int64) error {
	var err error
	if inputDir, err = expandHome(inputDir); err != nil {
		return err
	}
	if outMemiavlDir, err = expandHome(outMemiavlDir); err != nil {
		return err
	}
	if outFlatkvDir, err = expandHome(outFlatkvDir); err != nil {
		return err
	}

	snapshotDir, err := resolveSnapshotDir(inputDir, height)
	if err != nil {
		return err
	}

	modules, err := listModules(snapshotDir)
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return fmt.Errorf("no module subdirectories found in %s", snapshotDir)
	}
	fmt.Printf("source snapshot: %s\nmodules (%d): %v\n", snapshotDir, len(modules), modules)

	// Count total keys up front so progress reporting has a denominator. This
	// only mmaps each module snapshot and reads its leaf count (cheap); it does
	// not read the data.
	totalKeys, err := countKeys(snapshotDir, modules)
	if err != nil {
		return err
	}
	fmt.Printf("source DB contains %d keys total\n", totalKeys)

	// Ensure the destination directories exist before opening the stores.
	for _, dir := range []string{outMemiavlDir, outFlatkvDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory %s: %w", dir, err)
		}
	}

	// Destination memIAVL: create if missing, seed with the source module set.
	outMem, err := memiavl.OpenDB(0, memiavl.Options{
		Dir:             outMemiavlDir,
		CreateIfMissing: true,
		InitialStores:   modules,
		Config:          memiavl.DefaultConfig(),
	})
	if err != nil {
		return fmt.Errorf("open destination memiavl at %s: %w", outMemiavlDir, err)
	}
	defer func() { _ = outMem.Close() }()

	// Destination flatKV.
	fkvCfg := flatkvconfig.DefaultConfig()
	fkvCfg.DataDir = outFlatkvDir
	fkv, err := flatkv.NewCommitStore(context.Background(), fkvCfg)
	if err != nil {
		return fmt.Errorf("create destination flatkv at %s: %w", outFlatkvDir, err)
	}
	if _, err := fkv.LoadVersion(0, false); err != nil {
		return fmt.Errorf("open destination flatkv at %s: %w", outFlatkvDir, err)
	}
	defer func() { _ = fkv.Close() }()

	h := newHandler(outMem, fkv)

	startTime := time.Now()

	// Start a time-based progress reporter (logs roughly once per
	// progressInterval) and stop it when migration finishes.
	stop := make(chan struct{})
	var reporter sync.WaitGroup
	reporter.Add(1)
	go func() {
		defer reporter.Done()
		reportProgress(h, totalKeys, stop)
	}()

	for _, module := range modules {
		if err := migrateModule(snapshotDir, module, h); err != nil {
			close(stop)
			reporter.Wait()
			return fmt.Errorf("migrate module %q: %w", module, err)
		}
	}

	close(stop)
	reporter.Wait()

	elapsed := time.Since(startTime)
	fmt.Printf("migration complete: visited %d keys total in %s\n",
		h.Count(), elapsed.Round(time.Second))
	return nil
}

// countKeys opens each module snapshot (mmap only, no data read) and sums their
// leaf counts.
func countKeys(snapshotDir string, modules []string) (uint64, error) {
	var total uint64
	for _, module := range modules {
		snap, err := memiavl.OpenSnapshot(filepath.Join(snapshotDir, module), memiavl.Options{ZeroCopy: true})
		if err != nil {
			return 0, fmt.Errorf("open source snapshot for module %q: %w", module, err)
		}
		total += uint64(snap.LeavesLen())
		_ = snap.Close()
	}
	return total, nil
}

// reportProgress logs the number of keys visited on a fixed time interval until
// stop is closed.
func reportProgress(h *handler, total uint64, stop <-chan struct{}) {
	start := time.Now()
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()

	lastCount := uint64(0)
	lastTime := start
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			visited := h.Count()
			elapsed := now.Sub(start).Seconds()
			interval := now.Sub(lastTime).Seconds()

			var intervalRate float64
			if interval > 0 {
				intervalRate = float64(visited-lastCount) / interval
			}

			pct := 0.0
			if total > 0 {
				pct = float64(visited) / float64(total) * 100
			}

			fmt.Printf("[%6.0fs] visited %d / %d keys (%.1f%%), %.0f keys/s\n",
				elapsed, visited, total, pct, intervalRate)

			lastCount = visited
			lastTime = now
		}
	}
}

// migrateModule opens one module's snapshot, warms its page cache, and fans out
// numReaders goroutines that each scan a contiguous leaf-index range into a
// shared channel drained by a single consumer.
func migrateModule(snapshotDir, module string, h *handler) error {
	moduleDir := filepath.Join(snapshotDir, module)

	snap, err := memiavl.OpenSnapshot(moduleDir, memiavl.Options{ZeroCopy: true})
	if err != nil {
		return fmt.Errorf("open source snapshot: %w", err)
	}
	defer func() { _ = snap.Close() }()

	total := snap.LeavesLen()
	fmt.Printf("migrating module %q (%d keys)...\n", module, total)

	// Snapshots are mmap'd with MADV_RANDOM (no readahead). Our scan is fully
	// sequential by leaf index, so switch to MADV_SEQUENTIAL to get readahead.
	// This reads each file exactly once (vs. a separate prefetch pass that would
	// double the I/O) and lets the key counter advance continuously.
	snap.AdviseLeafScanSequential()

	ch := make(chan kvPair, channelCapacity)

	// Partition the leaf index space [0, total) into numReaders contiguous,
	// equal chunks. Partitioning by index (not key bytes) is distribution
	// agnostic, so prefixed keyspaces (e.g. the EVM store) stay balanced.
	var readers sync.WaitGroup
	for c := 0; c < numReaders; c++ {
		start := c * total / numReaders
		end := (c + 1) * total / numReaders
		if start >= end {
			continue
		}
		readers.Add(1)
		go func(start, end int) {
			defer readers.Done()
			_ = snap.ScanLeafRange(start, end, func(key, value []byte) error {
				ch <- kvPair{key: key, value: value}
				return nil
			})
		}(start, end)
	}

	// Close the channel once all readers have finished.
	go func() {
		readers.Wait()
		close(ch)
	}()

	// Single consumer. Runs until all readers are done and the channel drains.
	// The snapshot stays open (mmap valid) until this returns, so the zero-copy
	// key/value slices remain valid throughout.
	for kv := range ch {
		h.Handle(module, kv.key, kv.value)
	}

	return nil
}

// expandHome expands a leading "~" or "~/" in a path to the user's home
// directory. Paths without a leading tilde are returned unchanged. (A "~user"
// form is not supported.)
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// resolveSnapshotDir locates a memIAVL snapshot directory given the input path.
// It accepts several layouts:
//   - a full memIAVL tree with a "current" symlink (default for height == 0),
//   - a tree containing snapshot-<version> subdirectories (selected by --height,
//     otherwise the highest version),
//   - the snapshot directory itself (e.g. an extracted snapshot).
//
// A directory is recognized as a snapshot when it contains the multitree
// metadata file (memiavl.MetadataFileName).
func resolveSnapshotDir(inputDir string, height int64) (string, error) {
	// Explicit height: prefer the matching snapshot-<height> subdir.
	if height > 0 {
		candidate := filepath.Join(inputDir, fmt.Sprintf("%s%020d", memiavl.SnapshotPrefix, height))
		if isSnapshotDir(candidate) {
			return candidate, nil
		}
	}

	// Full memIAVL tree with a "current" symlink.
	if link, err := os.Readlink(filepath.Join(inputDir, "current")); err == nil {
		dir := link
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(inputDir, link)
		}
		if isSnapshotDir(dir) {
			return dir, nil
		}
	}

	// The input is itself a snapshot directory (e.g. an extracted snapshot).
	if isSnapshotDir(inputDir) {
		return inputDir, nil
	}

	// Otherwise look for snapshot-<version> subdirectories and pick the
	// requested height, or the highest version available.
	if dir := pickSnapshotSubdir(inputDir, height); dir != "" {
		return dir, nil
	}

	return "", fmt.Errorf(
		"could not locate a memIAVL snapshot under %s: expected a %q symlink, a %q metadata file, or %s* subdirectories",
		inputDir, "current", memiavl.MetadataFileName, memiavl.SnapshotPrefix)
}

// isSnapshotDir reports whether dir is a memIAVL multitree snapshot directory,
// identified by the presence of the multitree metadata file.
func isSnapshotDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, memiavl.MetadataFileName))
	return err == nil && !info.IsDir()
}

// pickSnapshotSubdir scans inputDir for snapshot-<version> subdirectories. If
// height > 0 it returns the matching one; otherwise it returns the highest
// version. Returns "" if none are found.
func pickSnapshotSubdir(inputDir string, height int64) string {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return ""
	}
	best := ""
	var bestVer int64 = -1
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), memiavl.SnapshotPrefix) {
			continue
		}
		ver, err := strconv.ParseInt(strings.TrimPrefix(e.Name(), memiavl.SnapshotPrefix), 10, 64)
		if err != nil {
			continue
		}
		dir := filepath.Join(inputDir, e.Name())
		if !isSnapshotDir(dir) {
			continue
		}
		if height > 0 {
			if ver == height {
				return dir
			}
			continue
		}
		if ver > bestVer {
			bestVer, best = ver, dir
		}
	}
	return best
}

// listModules returns the sorted module names (subdirectories) of a snapshot dir.
func listModules(snapshotDir string) ([]string, error) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return nil, fmt.Errorf("read snapshot dir %s: %w", snapshotDir, err)
	}
	var modules []string
	for _, e := range entries {
		if e.IsDir() {
			modules = append(modules, e.Name())
		}
	}
	sort.Strings(modules)
	return modules, nil
}

// handler consumes key/value pairs and writes them into the destination stores.
// Handle is invoked from the single consumer goroutine, but the visited counter
// is read concurrently by the progress reporter, so it is accessed atomically.
type handler struct {
	outMemIAVL *memiavl.DB
	outFlatKV  flatkv.Store

	// visited counts the total number of key/value pairs handled so far,
	// across all modules.
	visited atomic.Uint64
}

func newHandler(outMemIAVL *memiavl.DB, outFlatKV flatkv.Store) *handler {
	return &handler{outMemIAVL: outMemIAVL, outFlatKV: outFlatKV}
}

// Handle processes a single key/value pair read from the source memIAVL module.
//
// This is currently a placeholder that only tracks and reports progress.
//
// TODO: transform the pair as needed and write it into the destination memIAVL
// (h.outMemIAVL) and/or flatKV (h.outFlatKV) stores. Note that key/value are
// zero-copy views into the source snapshot mmap; copy them before retaining
// past the lifetime of the source snapshot.
func (h *handler) Handle(module string, key, value []byte) {
	// TODO: implement the actual write to outMemIAVL and outFlatKV.
	_ = module
	_ = key
	_ = value

	h.visited.Add(1)
}

// Count returns the total number of key/value pairs handled so far.
func (h *handler) Count() uint64 {
	return h.visited.Load()
}
