// Command staticmigrate performs an offline, static migration of a memIAVL
// file tree into a new memIAVL instance plus a flatKV file tree.
//
// Everything except the EVM module is staged wholesale: each non-EVM module
// subdirectory of the source snapshot is reproduced in a new snapshot at the
// same version H, so per-module versions and root hashes are preserved exactly
// with no tree iteration or rebuild. Files are hardlinked when the destination
// is on the same filesystem (instant, no data copied) and deep-copied
// otherwise. Only the small __metadata file is rewritten (to drop the evm
// store) and a fresh "current" symlink is created.
//
// Hardlinking is safe: memIAVL treats snapshot files as immutable. A node
// running on the destination opens them read-only and, when it later rewrites
// or prunes a snapshot, writes brand-new files and unlinks its own link.
// Unlinking a hardlink only drops the link count, so the source archive's bytes
// are never modified or deleted.
//
// The EVM module is the only one that changes format: its leaves are read
// directly (sequential I/O, no tree traversal) across N reader goroutines and
// streamed through the flatKV importer, which lands on the same version H.
//
// The source tree is only ever read; it is never modified, so the original
// files remain an immutable archive until manually deleted. The destination
// flatKV store takes a file lock, so any node using that directory must be
// stopped while this tool runs.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// numReaders is the number of parallel reader goroutines used to scan the EVM
// module's leaves: one per logical core. The scan is sequential within each
// disjoint leaf-index range, so this trades into NVMe queue depth; revisit if a
// machine has far more cores than the disk can usefully service in parallel.
var numReaders = runtime.NumCPU()

const (
	// channelCapacity is the buffer size of the channel between the EVM reader
	// goroutines and the single consumer. It only needs to smooth scheduling
	// jitter between the readers and the consumer: at steady state one side is
	// always the bottleneck, so a buffer beyond a few translate-batches' worth
	// just pins more mmap pages resident for no throughput gain. Sized at a
	// small multiple of translatorBatchSize.
	channelCapacity = 8 * translatorBatchSize

	// translatorBatchSize bounds how many memIAVL key/value pairs are handed to
	// a single flatkv.ImportTranslator.Translate call. Batching amortizes the
	// per-call map allocations across many keys. Distinct from the flatKV
	// importer's internal per-DB-worker flush threshold.
	translatorBatchSize = 2048

	// copyBufSize is the buffer size used for streamed file copies.
	copyBufSize = 4 << 20

	// progressInterval controls how often progress is logged (time-based, not
	// count-based).
	progressInterval = 10 * time.Second
)

// errScanStopped is returned by a reader's callback to abort a leaf scan early
// once the consumer has signaled (e.g. on an import error).
var errScanStopped = errors.New("scan stopped")

// kvPair is a single key/value record handed from a reader to the consumer.
// Key and value are zero-copy views into the source snapshot mmap.
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
	var (
		height int64
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "staticmigrate <input-memiavl> <out-memiavl> <out-flatkv>",
		Short: "Statically migrate a memIAVL file tree into a new memIAVL instance and a flatKV file tree",
		Long: `Statically migrate a memIAVL file tree.

Stages every non-EVM module of the source snapshot wholesale (at the same
version) into a new memIAVL instance, hardlinking files when possible and
deep-copying otherwise, and rebuilds the EVM module into a flatKV file tree at
the same version.

Arguments (all required, positional):
  input-memiavl   source memIAVL directory: either a full memIAVL tree
                  (e.g. .../state_commit/memiavl, with a 'current' symlink or
                  snapshot-<version> subdirectories) or an already-extracted
                  snapshot directory itself
  out-memiavl     destination memIAVL directory
  out-flatkv      destination flatKV directory

The source is read at a snapshot boundary (the 'current' snapshot by default,
or the snapshot for --height) and is never modified, so it remains an immutable
archive. Any un-snapshotted changelog (WAL) tail is not included.

The destination flatKV store takes a file lock; any node using that directory
must be stopped while this tool runs.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				return fmt.Errorf(
					"expected 3 positional arguments but received %d\n\n"+
						"  usage: %s\n\n"+
						"  input-memiavl   source memIAVL directory (e.g. .../state_commit/memiavl)\n"+
						"  out-memiavl     destination memIAVL directory\n"+
						"  out-flatkv      destination flatKV directory",
					len(args), cmd.UseLine())
			}
			return nil
		},
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return run(args[0], args[1], args[2], height, force)
		},
	}

	cmd.Flags().Int64Var(&height, "height", 0, "source snapshot version (0 = latest/current snapshot)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "delete the output directories first if they already exist (otherwise the tool errors out)")

	return cmd
}

func run(inputDir, outMemiavlDir, outFlatkvDir string, height int64, force bool) error {
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

	// Preflight: with -f, wipe any existing output dirs; without it, refuse to
	// run if either already exists. Done before touching anything else.
	if err := prepareOutputDirs(force, outMemiavlDir, outFlatkvDir); err != nil {
		return err
	}

	snapshotDir, err := resolveSnapshotDir(inputDir, height)
	if err != nil {
		return err
	}

	srcMeta, err := readSourceMetadata(snapshotDir)
	if err != nil {
		return err
	}
	h := srcMeta.CommitInfo.Version
	if h <= 0 {
		return fmt.Errorf("source snapshot %s has non-positive version %d", snapshotDir, h)
	}
	if height > 0 && height != h {
		return fmt.Errorf("requested --height %d but resolved snapshot is version %d", height, h)
	}

	modules, err := listModules(snapshotDir)
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return fmt.Errorf("no module subdirectories found in %s", snapshotDir)
	}

	var nonEVM []string
	hasEVM := false
	for _, m := range modules {
		if m == keys.EVMStoreKey {
			hasEVM = true
			continue
		}
		nonEVM = append(nonEVM, m)
	}

	fmt.Printf("source snapshot: %s (version %d)\n", snapshotDir, h)
	fmt.Printf("modules: %d total, %d non-evm to copy, evm present: %v\n", len(modules), len(nonEVM), hasEVM)

	start := time.Now()

	// Part A: copy all non-EVM modules wholesale into a new memIAVL snapshot.
	copyStart := time.Now()
	if err := copyNonEVMModules(snapshotDir, outMemiavlDir, h, nonEVM, srcMeta); err != nil {
		return fmt.Errorf("copy non-evm modules: %w", err)
	}
	copyElapsed := time.Since(copyStart)
	fmt.Printf("non-evm staging phase complete in %s\n", copyElapsed.Round(time.Second))

	// Part B: rebuild the EVM module into flatKV at the same version.
	var importElapsed time.Duration
	if hasEVM {
		importStart := time.Now()
		if err := importEVMToFlatKV(snapshotDir, outFlatkvDir, h); err != nil {
			return fmt.Errorf("import evm into flatkv: %w", err)
		}
		importElapsed = time.Since(importStart)
		fmt.Printf("evm import phase complete in %s\n", importElapsed.Round(time.Second))
	} else {
		fmt.Println("no evm module found in source; skipping flatkv import")
	}

	fmt.Printf("migration complete in %s (non-evm staging %s, evm import %s)\n",
		time.Since(start).Round(time.Second),
		copyElapsed.Round(time.Second),
		importElapsed.Round(time.Second))
	return nil
}

// prepareOutputDirs enforces the -f semantics. Pass 1 checks existence for all
// dirs (so nothing is touched before erroring when -f is not set); pass 2 wipes
// (when -f is set) and recreates each dir fresh.
func prepareOutputDirs(force bool, dirs ...string) error {
	for _, dir := range dirs {
		_, err := os.Stat(dir)
		if err == nil {
			if !force {
				return fmt.Errorf("output directory %s already exists; rerun with -f to overwrite", dir)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat output directory %s: %w", dir, err)
		}
	}
	for _, dir := range dirs {
		if force {
			if _, err := os.Stat(dir); err == nil {
				fmt.Printf("-f set: removing existing output directory %s\n", dir)
				if err := os.RemoveAll(dir); err != nil {
					return fmt.Errorf("remove existing output directory %s: %w", dir, err)
				}
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory %s: %w", dir, err)
		}
	}
	return nil
}

// copyNonEVMModules stages each non-EVM module subdirectory of the source
// snapshot into <outMemiavlDir>/snapshot-<h> (hardlinking files where possible,
// deep-copying otherwise), writes a filtered __metadata that drops the evm
// store, and points "current" at the new snapshot. The source is never modified
// either way, so it stays an immutable archive.
func copyNonEVMModules(snapshotDir, outMemiavlDir string, h int64, modules []string, srcMeta *proto.MultiTreeMetadata) error {
	snapName := fmt.Sprintf("%s%020d", memiavl.SnapshotPrefix, h)
	destSnap := filepath.Join(outMemiavlDir, snapName)
	if err := os.MkdirAll(destSnap, 0o755); err != nil {
		return fmt.Errorf("create destination snapshot dir %s: %w", destSnap, err)
	}

	var totalBytes uint64
	for _, m := range modules {
		sz, err := dirSize(filepath.Join(snapshotDir, m))
		if err != nil {
			return fmt.Errorf("size module %q: %w", m, err)
		}
		totalBytes += sz
	}
	fmt.Printf("staging %d non-evm modules (%.2f MiB) into %s (hardlinking where possible)\n",
		len(modules), mib(totalBytes), destSnap)

	var copied atomic.Uint64
	var staged stageStats
	stopReporter := startReporter(totalBytes, copied.Load, formatBytesProgress)
	for _, m := range modules {
		if err := stageDir(filepath.Join(snapshotDir, m), filepath.Join(destSnap, m), &copied, &staged); err != nil {
			stopReporter()
			return fmt.Errorf("stage module %q: %w", m, err)
		}
	}
	stopReporter()
	fmt.Printf("staged %.2f MiB across %d modules (%d files hardlinked, %d files copied)\n",
		mib(copied.Load()), len(modules), staged.linked.Load(), staged.copiedFile.Load())

	if err := writeFilteredMetadata(destSnap, srcMeta); err != nil {
		return err
	}
	if err := writeCurrentSymlink(outMemiavlDir, snapName); err != nil {
		return fmt.Errorf("write current symlink: %w", err)
	}
	fmt.Printf("wrote %s and current -> %s\n", memiavl.MetadataFileName, snapName)
	return nil
}

// importEVMToFlatKV reads the source evm module's leaves in parallel and streams
// them through the flatKV importer, finalizing at version height. The named
// return err is inspected by the deferred importer cleanup: on error we Abort
// (so the partial import is not committed), otherwise we Close (which finalizes
// the snapshot at height).
func importEVMToFlatKV(snapshotDir, outFlatkvDir string, height int64) (err error) {
	evmDir := filepath.Join(snapshotDir, keys.EVMStoreKey)
	snap, serr := memiavl.OpenSnapshot(evmDir, memiavl.Options{ZeroCopy: true})
	if serr != nil {
		return fmt.Errorf("open evm snapshot: %w", serr)
	}
	defer func() { _ = snap.Close() }()

	total := snap.LeavesLen()
	fmt.Printf("importing evm module into flatkv (%d keys) at version %d\n", total, height)

	// Snapshots are mmap'd with MADV_RANDOM (no readahead). Our scan is fully
	// sequential by leaf index, so switch to MADV_SEQUENTIAL for readahead.
	snap.AdviseLeafScanSequential()

	ctx := context.Background()
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = outFlatkvDir
	// This is a one-shot, restartable bulk import: a crash just means rerunning
	// against the immutable source archive. Trade durability for write
	// throughput (WAL off, large memtable, parallel/relaxed compaction). The
	// importer flushes the data DBs before snapshotting to stay correct with the
	// WAL disabled.
	cfg.ApplyBulkImportProfile()
	store, serr := flatkv.NewCommitStore(ctx, cfg)
	if serr != nil {
		return fmt.Errorf("create flatkv store: %w", serr)
	}
	defer func() { _ = store.Close() }()
	if _, serr := store.LoadVersion(0, false); serr != nil {
		return fmt.Errorf("open flatkv store: %w", serr)
	}

	importer, ierr := store.Importer(height)
	if ierr != nil {
		return fmt.Errorf("create flatkv importer: %w", ierr)
	}
	defer func() {
		if err != nil {
			// Do NOT Close on the error path: that would finalize a partial
			// import at the target version. Abort drains workers without
			// writing a snapshot, leaving flatKV at its pre-import version.
			if kvi, ok := importer.(*flatkv.KVImporter); ok {
				_ = kvi.Abort(err)
			}
			return
		}
		if cerr := importer.Close(); cerr != nil {
			err = fmt.Errorf("finalize flatkv import: %w", cerr)
		}
	}()

	// visited counts memIAVL source leaves fully handled (batched/encoded for
	// the importer). Every phase below increments it, so it rises to total and
	// the meter tracks end-to-end progress across all phases.
	var visited atomic.Uint64
	stopReporter := startReporter(uint64(total), visited.Load, formatKeysProgress)
	defer stopReporter()

	// Locate each EVM data type's contiguous leaf range. Leaves are key-sorted,
	// so storage, code, codehash, and nonce are each one contiguous run; legacy
	// data (address mappings, codesize, receipts, ...) is everything else, i.e.
	// the complement of those four ranges.
	storageLo, storageHi, srerr := kindLeafRange(snap, keys.EVMKeyStorage)
	if srerr != nil {
		return fmt.Errorf("locate evm storage range: %w", srerr)
	}
	codeLo, codeHi, crerr := kindLeafRange(snap, keys.EVMKeyCode)
	if crerr != nil {
		return fmt.Errorf("locate evm code range: %w", crerr)
	}
	nonceRange, codeHashRange, arerr := accountLeafRanges(snap)
	if arerr != nil {
		return fmt.Errorf("locate evm account ranges: %w", arerr)
	}
	legacyIntervals := complementIntervals(total, [][2]int{
		{storageLo, storageHi}, {codeLo, codeHi}, codeHashRange, nonceRange,
	})

	// Process each data type in its own phase, sequentially. Only one flatKV DB
	// (and therefore one hash pool) is active per phase, so hashing always has
	// the full core count rather than being oversubscribed by overlapping types.
	var totalWritten int64
	runScan := func(name string, intervals [][2]int) error {
		start := time.Now()
		read, written, e := scanAndTranslate(snap, importer, height, intervals, &visited)
		if e != nil {
			return fmt.Errorf("%s phase: %w", name, e)
		}
		totalWritten += written
		fmt.Printf("evm %s phase: read %d keys, wrote %d rows in %s\n",
			name, read, written, time.Since(start).Round(time.Second))
		return nil
	}

	if err = runScan("storage", [][2]int{{storageLo, storageHi}}); err != nil {
		return err
	}
	if err = runScan("code", [][2]int{{codeLo, codeHi}}); err != nil {
		return err
	}
	if err = runScan("legacy", legacyIntervals); err != nil {
		return err
	}

	// Account phase: merge-join the nonce and codehash ranges into account rows.
	accStart := time.Now()
	var accountRows atomic.Int64
	accDone := make(chan struct{})
	accErr := runAccountProducers(snap, importer, height, numReaders,
		nonceRange, codeHashRange, &visited, &accountRows, accDone)
	close(accDone)
	if accErr != nil {
		err = fmt.Errorf("accounts phase: %w", accErr)
		return err
	}
	if eerr := importerErr(importer); eerr != nil {
		err = fmt.Errorf("flatkv import failed: %w", eerr)
		return err
	}
	totalWritten += accountRows.Load()
	fmt.Printf("evm accounts phase: wrote %d account rows in %s\n",
		accountRows.Load(), time.Since(accStart).Round(time.Second))

	stopReporter()
	// importer.Close() runs in the deferred cleanup (success path), finalizing
	// the flatKV snapshot at version height.
	fmt.Printf("evm import: handled %d source keys, wrote %d flatkv rows\n",
		visited.Load(), totalWritten)
	return nil
}

// scanAndTranslate runs one non-account phase: it fans numReaders readers over
// the given leaf intervals into a shared channel and feeds the rows through a
// single ImportTranslator to the importer. Account keys must not appear in
// intervals (they are owned by the account phase); any that slip through are
// dropped defensively and Finalize is asserted empty. visited is incremented
// per leaf consumed. Returns the leaves read and rows written.
func scanAndTranslate(snap *memiavl.Snapshot, importer sctypes.Importer, height int64, intervals [][2]int, visited *atomic.Uint64) (read, written int64, err error) {
	readerParts := splitIntervals(intervals, numReaders)
	if len(readerParts) == 0 {
		return 0, 0, nil
	}

	ch := make(chan kvPair, channelCapacity)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }
	defer stop()

	var readers sync.WaitGroup
	for _, parts := range readerParts {
		readers.Add(1)
		go func(parts [][2]int) {
			defer readers.Done()
			for _, iv := range parts {
				if e := snap.ScanLeafRange(iv[0], iv[1], func(key, value []byte) error {
					select {
					case ch <- kvPair{key: key, value: value}:
						return nil
					case <-done:
						return errScanStopped
					}
				}); e != nil {
					return
				}
			}
		}(parts)
	}
	go func() {
		readers.Wait()
		close(ch)
	}()

	translator := flatkv.NewImportTranslator(height)
	batch := &proto.NamedChangeSet{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: make([]*proto.KVPair, 0, translatorBatchSize)},
	}
	flush := func() error {
		if len(batch.Changeset.Pairs) == 0 {
			return nil
		}
		pairs, terr := translator.Translate(batch)
		if terr != nil {
			return fmt.Errorf("translate evm batch: %w", terr)
		}
		written += emitPairs(importer, pairs, height)
		batch.Changeset.Pairs = batch.Changeset.Pairs[:0]
		return nil
	}

	for kv := range ch {
		visited.Add(1)
		if kind, _ := keys.ParseEVMKey(kv.key); kind == keys.EVMKeyNonce || kind == keys.EVMKeyCodeHash {
			// Defensive: accounts belong to the account phase.
			continue
		}
		read++
		batch.Changeset.Pairs = append(batch.Changeset.Pairs, &proto.KVPair{Key: kv.key, Value: kv.value})
		if len(batch.Changeset.Pairs) >= translatorBatchSize {
			if ferr := flush(); ferr != nil {
				err = ferr
				break
			}
			if eerr := importerErr(importer); eerr != nil {
				err = fmt.Errorf("flatkv import failed: %w", eerr)
				break
			}
		}
	}
	if err != nil {
		stop()
		drain(ch)
		return read, written, err
	}

	if ferr := flush(); ferr != nil {
		return read, written, ferr
	}
	if leftover := translator.Finalize(); len(leftover) != 0 {
		return read, written, fmt.Errorf("translator buffered %d account rows in a non-account phase; expected 0", len(leftover))
	}
	return read, written, nil
}

// drain consumes any remaining items so reader goroutines blocked on a send can
// observe the done signal and exit, letting the channel-closer goroutine finish.
func drain(ch <-chan kvPair) {
	for range ch {
	}
}

// importerErr surfaces any pipeline error the flatKV importer's worker
// goroutines have already recorded, so the consumer can fail fast.
func importerErr(importer sctypes.Importer) error {
	if e, ok := importer.(interface{ Err() error }); ok {
		return e.Err()
	}
	return nil
}

// emitPairs forwards translator output to the flatKV importer, returning the
// number of pairs written.
func emitPairs(importer sctypes.Importer, pairs []flatkv.PhysicalKVPair, height int64) int64 {
	for _, p := range pairs {
		importer.AddNode(&sctypes.SnapshotNode{
			Key:     p.Key,
			Value:   p.Value,
			Version: height,
			Height:  0,
		})
	}
	return int64(len(pairs))
}

// readSourceMetadata reads and unmarshals the multitree __metadata file of a
// snapshot directory.
func readSourceMetadata(snapshotDir string) (*proto.MultiTreeMetadata, error) {
	bz, err := os.ReadFile(filepath.Join(snapshotDir, memiavl.MetadataFileName))
	if err != nil {
		return nil, fmt.Errorf("read source metadata: %w", err)
	}
	var md proto.MultiTreeMetadata
	if err := md.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("unmarshal source metadata: %w", err)
	}
	if md.CommitInfo == nil {
		return nil, fmt.Errorf("source metadata %s is missing commit info", snapshotDir)
	}
	return &md, nil
}

// writeFilteredMetadata writes a copy of src into destSnapshotDir with the evm
// store removed from the commit info, preserving Version and InitialVersion.
func writeFilteredMetadata(destSnapshotDir string, src *proto.MultiTreeMetadata) error {
	infos := make([]proto.StoreInfo, 0, len(src.CommitInfo.StoreInfos))
	for _, si := range src.CommitInfo.StoreInfos {
		if si.Name == keys.EVMStoreKey {
			continue
		}
		infos = append(infos, si)
	}
	md := proto.MultiTreeMetadata{
		InitialVersion: src.InitialVersion,
		CommitInfo: &proto.CommitInfo{
			Version:    src.CommitInfo.Version,
			StoreInfos: infos,
		},
	}
	bz, err := md.Marshal()
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := memiavl.WriteFileSync(filepath.Join(destSnapshotDir, memiavl.MetadataFileName), bz); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// writeCurrentSymlink atomically points <root>/current at the (relative)
// snapshot directory name, matching memIAVL's own convention.
func writeCurrentSymlink(root, snapshotName string) error {
	tmp := filepath.Join(root, "current-tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(snapshotName, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, "current"))
}

// dirSize sums the sizes of all regular files under dir.
func dirSize(dir string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += uint64(info.Size())
		}
		return nil
	})
	return total, err
}

// stageDir recursively reproduces src at dst. Regular files are hardlinked when
// possible (instant, no data copied) and deep-copied otherwise. Hardlinking is
// safe here because memIAVL treats snapshot files as immutable: a node running
// on the destination only ever reads these inodes or unlinks its own link, so
// the source archive's bytes are never modified. The link/copy choice is
// recorded in staged so callers can report which path was taken.
func stageDir(src, dst string, copied *atomic.Uint64, staged *stageStats) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		switch {
		case e.IsDir():
			if err := stageDir(s, d, copied, staged); err != nil {
				return err
			}
		case e.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(s)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, d); err != nil {
				return err
			}
		default:
			if err := linkOrCopyFile(s, d, copied, staged); err != nil {
				return err
			}
		}
	}
	fsyncDir(dst)
	return nil
}

// stageStats tracks, across a staging run, how many files were hardlinked vs.
// deep-copied. linkedOnce guards the one-time cross-device fallback notice.
type stageStats struct {
	linked     atomic.Uint64
	copiedFile atomic.Uint64
	notifyOnce sync.Once
}

// linkOrCopyFile hardlinks src to dst, falling back to a deep byte copy if the
// link cannot be created (e.g. the destination is on a different filesystem,
// EXDEV, or the filesystem disallows hardlinks). On a successful link no data is
// copied; the file's size is still added to copied so progress reflects the
// amount of data staged.
func linkOrCopyFile(src, dst string, copied *atomic.Uint64, staged *stageStats) error {
	if err := os.Link(src, dst); err == nil {
		if info, serr := os.Lstat(dst); serr == nil {
			copied.Add(uint64(info.Size()))
		}
		staged.linked.Add(1)
		return nil
	} else if errors.Is(err, syscall.EXDEV) {
		staged.notifyOnce.Do(func() {
			fmt.Println("note: destination is on a different filesystem than the source; " +
				"hardlinks unavailable, falling back to a deep byte copy (slower)")
		})
	}
	staged.copiedFile.Add(1)
	return copyFileContents(src, dst, copied)
}

// copyFileContents copies a single regular file, preserving its permission bits
// and fsync-ing the destination.
func copyFileContents(src, dst string, copied *atomic.Uint64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}

	buf := make([]byte, copyBufSize)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				return werr
			}
			copied.Add(uint64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			return rerr
		}
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// fsyncDir flushes a directory entry to disk. Best effort: some filesystems do
// not support directory fsync.
func fsyncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// startReporter launches a goroutine that logs progress every progressInterval
// until the returned stop function is called. The stop function is idempotent.
func startReporter(total uint64, get func() uint64, format func(elapsed float64, cur, total uint64, rate float64) string) func() {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
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
				cur := get()
				interval := now.Sub(lastTime).Seconds()
				rate := 0.0
				if interval > 0 {
					rate = float64(cur-lastCount) / interval
				}
				fmt.Println(format(now.Sub(start).Seconds(), cur, total, rate))
				lastCount = cur
				lastTime = now
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			wg.Wait()
		})
	}
}

func formatBytesProgress(elapsed float64, cur, total uint64, rate float64) string {
	pct := 0.0
	if total > 0 {
		pct = float64(cur) / float64(total) * 100
	}
	return fmt.Sprintf("[%6.0fs] copied %.2f / %.2f MiB (%.1f%%), %.1f MiB/s",
		elapsed, mib(cur), mib(total), pct, rate/(1<<20))
}

func formatKeysProgress(elapsed float64, cur, total uint64, rate float64) string {
	pct := 0.0
	if total > 0 {
		pct = float64(cur) / float64(total) * 100
	}
	return fmt.Sprintf("[%6.0fs] imported %d / %d evm keys (%.1f%%), %.0f keys/s",
		elapsed, cur, total, pct, rate)
}

func mib(b uint64) float64 {
	return float64(b) / (1 << 20)
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
