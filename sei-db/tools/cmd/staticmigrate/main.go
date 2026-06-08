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
	"sync"

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
)

// kvPair is a single key/value record handed from a reader to the consumer.
type kvPair struct {
	key   []byte
	value []byte
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		inputDir      string
		outMemiavlDir string
		outFlatkvDir  string
		height        int64
	)

	cmd := &cobra.Command{
		Use:   "staticmigrate",
		Short: "Statically migrate a memIAVL file tree into a new memIAVL instance and a flatKV file tree",
		Long: `Statically migrate a memIAVL file tree.

Reads the source memIAVL snapshot module-by-module, scanning each module's
leaf records directly across multiple goroutines, and feeds every key/value
pair through a single handler that writes into a new memIAVL instance and a
flatKV file tree.

The source is read at a snapshot boundary (the 'current' snapshot by default,
or the snapshot for --height). Any un-snapshotted changelog (WAL) tail is not
included.

The destination stores take file locks; any node using these directories must
be stopped while this tool runs.`,
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if inputDir == "" || outMemiavlDir == "" || outFlatkvDir == "" {
				return fmt.Errorf("--input, --out-memiavl and --out-flatkv are all required")
			}
			return run(inputDir, outMemiavlDir, outFlatkvDir, height)
		},
	}

	cmd.Flags().StringVarP(&inputDir, "input", "i", "", "source memIAVL directory (e.g. .../state_commit/memiavl)")
	cmd.Flags().StringVar(&outMemiavlDir, "out-memiavl", "", "destination memIAVL directory")
	cmd.Flags().StringVar(&outFlatkvDir, "out-flatkv", "", "destination flatKV directory")
	cmd.Flags().Int64Var(&height, "height", 0, "source snapshot version (0 = latest/current snapshot)")

	return cmd
}

func run(inputDir, outMemiavlDir, outFlatkvDir string, height int64) error {
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

	for _, module := range modules {
		if err := migrateModule(snapshotDir, module, h); err != nil {
			return fmt.Errorf("migrate module %q: %w", module, err)
		}
	}

	fmt.Println("migration complete")
	return nil
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

	// Warm the page cache: a single sequential pass over leaves+kvs converts the
	// otherwise random mmap page faults into sequential reads. This is the main
	// speedup over a tree-traversal iterator.
	_ = memiavl.SequentialReadAndFillPageCache(filepath.Join(moduleDir, memiavl.FileNameLeaves))
	_ = memiavl.SequentialReadAndFillPageCache(filepath.Join(moduleDir, memiavl.FileNameKVs))

	total := snap.LeavesLen()
	fmt.Printf("migrating module %q (%d keys)\n", module, total)

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

// resolveSnapshotDir returns the snapshot directory inside a memIAVL file tree.
// For height == 0 it follows the "current" symlink; otherwise it constructs the
// snapshot-<version> directory name.
func resolveSnapshotDir(inputDir string, height int64) (string, error) {
	var name string
	if height > 0 {
		name = fmt.Sprintf("%s%020d", memiavl.SnapshotPrefix, height)
	} else {
		link, err := os.Readlink(filepath.Join(inputDir, "current"))
		if err != nil {
			return "", fmt.Errorf("read 'current' symlink in %s: %w", inputDir, err)
		}
		name = filepath.Base(link)
	}

	dir := filepath.Join(inputDir, name)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("stat snapshot dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("snapshot path %s is not a directory", dir)
	}
	return dir, nil
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
type handler struct {
	outMemIAVL *memiavl.DB
	outFlatKV  flatkv.Store
}

func newHandler(outMemIAVL *memiavl.DB, outFlatKV flatkv.Store) *handler {
	return &handler{outMemIAVL: outMemIAVL, outFlatKV: outFlatKV}
}

// Handle processes a single key/value pair read from the source memIAVL module.
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
}
