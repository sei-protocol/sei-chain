package operations

// seidb snapshot-roundtrip — drive the composite state-sync exporter/importer
// end-to-end in one process.
//
// WHY THIS EXISTS
// ---------------
// Production state-sync works like this:
//   (A) source node  ->  composite.Exporter  ->  ABCI chunk stream  ->
//       destination node  ->  composite.Importer  ->  rematerialized dirs
//
// The ABCI chunk-stream layer is cosmos-sdk plumbing; the layer we actually
// own and need to prove lossless is the seidb half: composite.Exporter +
// composite.Importer fanning out to memIAVL and FlatKV committers. That's
// also the exact layer where FlatKV / memIAVL can silently disagree.
//
// This command skips the network and the cosmos-sdk chunker and drives the
// seidb half in-process: open the SOURCE's memIAVL + FlatKV in no-contention
// read-only mode, run composite.Exporter on the src side and composite.Importer
// on a freshly-created DESTINATION dir, and drain one into the other. After
// the roundtrip the caller can `seidb flatkv-account` both sides for any
// account and get byte-identical JSON if the path is correct.
//
// WHY NOT BUILD A FULL COMPOSITE STORE ON THE SRC SIDE
// ----------------------------------------------------
// Opening a live node's committer.db with a writer handle would fight the
// node for memIAVL's flock and Pebble's writer lock. Instead we construct
// the two leg-exporters independently (memIAVL goes through its own RO
// OpenDB path; FlatKV goes through the clone-to-tempdir helper shared with
// flatkv-account / dump-flatkv) and merge them with composite.NewExporter.
// The dst side we own outright, so we build a real CompositeCommitStore
// there exactly as production does.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/spf13/cobra"
)

func SnapshotRoundtripCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot-roundtrip",
		Short: "Drive composite state-sync export+import end-to-end into a fresh destination dir",
		Long: `Opens the source memIAVL + FlatKV at --version in no-contention mode
(no write-lock acquisition), constructs a composite exporter, and streams it
into a composite importer backed by a freshly-created destination home dir
(at <dst-home>/data/committer.db and <dst-home>/data/flatkv).

On success the destination dir contains a rematerialized snapshot at --version
that can be opened as a normal seid data dir. Pair with seidb flatkv-account /
iavl-account against both src and dst to prove byte-identical state.

--cosmos-only skips the FlatKV leg (equivalent to running on a cosmos_only
node). In that mode --src-flatkv is ignored and no FlatKV dir is created
under --dst-home.`,
		Run: executeSnapshotRoundtrip,
	}

	cmd.Flags().String("src-memiavl", "", "source memIAVL dir (e.g. /root/.sei/data/committer.db)")
	cmd.Flags().String("src-flatkv", "", "source FlatKV dir (e.g. /root/.sei/data/flatkv); ignored with --cosmos-only")
	cmd.Flags().Int64("version", 0, "version (block height) to export; must equal an existing memIAVL snapshot version")
	cmd.Flags().String("dst-home", "", "destination home dir; <dst-home>/data/{committer.db,flatkv} will be populated")
	cmd.Flags().Bool("cosmos-only", false, "skip FlatKV leg; roundtrip memIAVL only")
	cmd.Flags().Bool("verify-reopen", true, "after import, reopen dst and print final version + per-tree root hashes")

	_ = cmd.MarkFlagRequired("src-memiavl")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("dst-home")
	return cmd
}

func executeSnapshotRoundtrip(cmd *cobra.Command, _ []string) {
	srcMemiavlDir, _ := cmd.Flags().GetString("src-memiavl")
	srcFlatKVDir, _ := cmd.Flags().GetString("src-flatkv")
	version, _ := cmd.Flags().GetInt64("version")
	dstHome, _ := cmd.Flags().GetString("dst-home")
	cosmosOnly, _ := cmd.Flags().GetBool("cosmos-only")
	verifyReopen, _ := cmd.Flags().GetBool("verify-reopen")

	if version <= 0 {
		panic("--version must be positive")
	}
	if !cosmosOnly && srcFlatKVDir == "" {
		panic("--src-flatkv is required unless --cosmos-only is set")
	}
	if err := ensureEmptyOrNew(dstHome); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	treeNames, err := listMemiavlTreeNames(srcMemiavlDir, version)
	if err != nil {
		panic(fmt.Errorf("list memiavl tree names: %w", err))
	}
	if len(treeNames) == 0 {
		panic(fmt.Errorf("no memiavl trees found at %s version=%d", srcMemiavlDir, version))
	}

	start := time.Now()
	fmt.Printf("snapshot-roundtrip: src=%s flatkv=%s version=%d dst=%s cosmos-only=%v\n",
		srcMemiavlDir, srcFlatKVDir, version, dstHome, cosmosOnly)
	fmt.Printf("  memiavl trees (%d): %v\n", len(treeNames), treeNames)

	srcExporter, srcFKVClone := openSrcCompositeExporter(srcMemiavlDir, srcFlatKVDir, version, cosmosOnly)
	defer func() {
		if srcExporter != nil {
			_ = srcExporter.Close()
		}
		if srcFKVClone != nil {
			_ = srcFKVClone.Close()
		}
	}()

	dstImporter, dstStore := openDstCompositeImporter(ctx, dstHome, treeNames, version, cosmosOnly)

	stats := drainExporterIntoImporter(srcExporter, dstImporter)

	if err := dstImporter.Close(); err != nil {
		panic(fmt.Errorf("dst importer close: %w", err))
	}
	if err := dstStore.Close(); err != nil {
		panic(fmt.Errorf("dst store close after import: %w", err))
	}

	elapsed := time.Since(start)
	fmt.Printf("roundtrip OK in %s: %d modules, %d nodes\n", elapsed.Round(time.Millisecond), stats.modules, stats.totalNodes)
	for _, mod := range stats.orderedModules {
		fmt.Printf("  %-20s %d nodes\n", mod, stats.perModuleNodes[mod])
	}

	if verifyReopen {
		verifyDstReopen(ctx, dstHome, treeNames, version, cosmosOnly)
	}
}

// ---------------------------------------------------------------------------
// src side: independent memiavl + flatkv exporters merged via composite
// ---------------------------------------------------------------------------

func openSrcCompositeExporter(srcMemiavlDir, srcFlatKVDir string, version int64, cosmosOnly bool) (types.Exporter, *openedFlatKV) {
	if version > int64(^uint32(0)) {
		panic(fmt.Errorf("version %d overflows uint32", version))
	}

	cosmosExporter, err := memiavl.NewMultiTreeExporter(srcMemiavlDir, uint32(version), false)
	if err != nil {
		panic(fmt.Errorf("open src memiavl exporter: %w", err))
	}

	var flatExporter types.Exporter
	var srcFKVClone *openedFlatKV
	if !cosmosOnly {
		srcFKVClone, err = openFlatKVReadOnly(srcFlatKVDir, version)
		if err != nil {
			_ = cosmosExporter.Close()
			panic(fmt.Errorf("open src flatkv clone: %w", err))
		}
		flatExporter, err = srcFKVClone.Exporter(version)
		if err != nil {
			_ = cosmosExporter.Close()
			_ = srcFKVClone.Close()
			panic(fmt.Errorf("build flatkv exporter: %w", err))
		}
	}

	merged, err := composite.NewExporter(cosmosExporter, flatExporter)
	if err != nil {
		_ = cosmosExporter.Close()
		if flatExporter != nil {
			_ = flatExporter.Close()
		}
		if srcFKVClone != nil {
			_ = srcFKVClone.Close()
		}
		panic(fmt.Errorf("build composite exporter: %w", err))
	}
	return merged, srcFKVClone
}

// listMemiavlTreeNames opens the src memIAVL dir read-only for just long
// enough to read the snapshot's tree list, then closes it. We need the list
// up front because CompositeCommitStore.Initialize wants every store name
// declared before LoadVersion runs.
func listMemiavlTreeNames(dir string, version int64) ([]string, error) {
	db, err := memiavl.OpenDB(version, memiavl.Options{
		Dir:             dir,
		ZeroCopy:        true,
		CreateIfMissing: false,
		ReadOnly:        true,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	trees := db.MultiTree.Trees()
	names := make([]string, 0, len(trees))
	for _, nt := range trees {
		names = append(names, nt.Name)
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// dst side: full composite store, importer-ready
// ---------------------------------------------------------------------------

func openDstCompositeImporter(
	ctx context.Context,
	dstHome string,
	treeNames []string,
	version int64,
	cosmosOnly bool,
) (types.Importer, *composite.CompositeCommitStore) {
	cfg := config.DefaultStateCommitConfig()
	if !cosmosOnly {
		cfg.WriteMode = config.DualWrite
	}
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	if err := os.MkdirAll(filepath.Join(dstHome, "data", "committer.db"), 0o750); err != nil {
		panic(fmt.Errorf("mkdir dst committer.db: %w", err))
	}
	if !cosmosOnly {
		if err := os.MkdirAll(filepath.Join(dstHome, "data", "flatkv"), 0o750); err != nil {
			panic(fmt.Errorf("mkdir dst flatkv: %w", err))
		}
	}

	dst := composite.NewCompositeCommitStore(ctx, dstHome, cfg)
	dst.Initialize(treeNames)

	// Initial LoadVersion creates the underlying DB structures at version 0
	// so the subsequent Importer call has something concrete to reset. We
	// then Close() before Importer per the canonical test pattern — the
	// importer is itself responsible for reopening via resetForImport.
	if _, err := dst.LoadVersion(0, false); err != nil {
		panic(fmt.Errorf("dst initial LoadVersion: %w", err))
	}
	if err := dst.Close(); err != nil {
		panic(fmt.Errorf("dst close before import: %w", err))
	}

	imp, err := dst.Importer(version)
	if err != nil {
		panic(fmt.Errorf("build dst importer: %w", err))
	}
	return imp, dst
}

// ---------------------------------------------------------------------------
// drain loop
// ---------------------------------------------------------------------------

type roundtripStats struct {
	modules        int
	totalNodes     int64
	orderedModules []string
	perModuleNodes map[string]int64
}

func drainExporterIntoImporter(src types.Exporter, dst types.Importer) roundtripStats {
	stats := roundtripStats{perModuleNodes: map[string]int64{}}

	var currentModule string
	var reportEvery int64 = 500_000
	var nextReport int64 = reportEvery

	for {
		raw, err := src.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				break
			}
			panic(fmt.Errorf("exporter Next: %w", err))
		}
		switch v := raw.(type) {
		case string:
			if err := dst.AddModule(v); err != nil {
				panic(fmt.Errorf("importer AddModule(%q): %w", v, err))
			}
			currentModule = v
			stats.modules++
			stats.orderedModules = append(stats.orderedModules, v)
			fmt.Printf("  -> module: %s\n", v)
		case *types.SnapshotNode:
			dst.AddNode(v)
			stats.totalNodes++
			stats.perModuleNodes[currentModule]++
			if stats.totalNodes >= nextReport {
				fmt.Printf("     [%s] %d nodes piped so far\n", currentModule, stats.perModuleNodes[currentModule])
				nextReport += reportEvery
			}
		default:
			panic(fmt.Errorf("unexpected exporter item %T", raw))
		}
	}
	return stats
}

// ---------------------------------------------------------------------------
// post-import sanity reopen
// ---------------------------------------------------------------------------

func verifyDstReopen(
	ctx context.Context,
	dstHome string,
	treeNames []string,
	version int64,
	cosmosOnly bool,
) {
	cfg := config.DefaultStateCommitConfig()
	if !cosmosOnly {
		cfg.WriteMode = config.DualWrite
	}
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	dst := composite.NewCompositeCommitStore(ctx, dstHome, cfg)
	dst.Initialize(treeNames)
	if _, err := dst.LoadVersion(version, false); err != nil {
		panic(fmt.Errorf("reopen dst at version %d: %w", version, err))
	}
	defer func() { _ = dst.Close() }()

	fmt.Printf("reopened dst: version=%d\n", dst.Version())
	ci := dst.LastCommitInfo()
	if ci != nil {
		fmt.Printf("  last_commit: version=%d stores=%d\n", ci.Version, len(ci.StoreInfos))
		for _, si := range ci.StoreInfos {
			fmt.Printf("  %-20s %X\n", si.Name, si.CommitId.Hash)
		}
	}
	if !cosmosOnly {
		if fkv := dst.FlatKVStore(); fkv != nil {
			fmt.Printf("  flatkv root: %X (module key=%s)\n", fkv.RootHash(), keys.EVMStoreKey)
		}
	}
}

// ensureEmptyOrNew accepts a non-existent dst-home or an empty directory,
// and rejects anything that already has seidb artifacts under it so we
// don't accidentally trample a live node's data.
func ensureEmptyOrNew(dstHome string) error {
	if dstHome == "" {
		return fmt.Errorf("--dst-home must not be empty")
	}
	info, err := os.Stat(dstHome)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat dst-home %s: %w", dstHome, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("dst-home %s exists but is not a directory", dstHome)
	}
	// Reject pre-existing seidb data.
	for _, sub := range []string{"data/committer.db", "data/flatkv"} {
		p := filepath.Join(dstHome, sub)
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("dst-home %s already contains %s; refusing to overwrite", dstHome, sub)
		}
	}
	return nil
}
