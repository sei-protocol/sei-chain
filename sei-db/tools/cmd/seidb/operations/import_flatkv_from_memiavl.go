package operations

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/spf13/cobra"
)

// translatorBatchSize bounds how many memiavl key/value pairs we hand to a
// single flatkv.ImportTranslator.Translate call. Batching amortizes the
// per-call classifyAndPrefix map allocations across many keys without
// growing ImportTranslator's account-buffer memory beyond what an unbatched
// stream would already need.
//
// Distinct from flatkv.importBatchSize, which is the per-DB-worker flush
// threshold (in already-translated physical pairs); the two constants tune
// different stages of the pipeline.
const translatorBatchSize = 2048

// ImportFlatKVFromMemiavlCmd imports selected memiavl modules into FlatKV.
//
// This is an offline test-seeding and pre-migration recovery utility. Typical
// uses are constructing FlatKV fixtures from an existing memiavl-only data
// directory, testing memiavl-to-FlatKV encoding, or rebuilding FlatKV from a
// pre-migration memiavl backup without replaying from genesis. It is not the
// production MigrationManager path and does not perform state sync or write
// migration metadata.
//
// The supported scope is intentionally narrow: only the evm module is
// accepted. Non-EVM modules remain in memiavl and are not copied into FlatKV.
// Importing resets FlatKV and replaces it with the selected memiavl data; the
// CLI refuses to run over existing FlatKV data unless --force is supplied. The
// source node must be stopped; the command holds memiavl's writer lock for the
// full import and fails if a live node already owns it.
func ImportFlatKVFromMemiavlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-flatkv-from-memiavl",
		Short: "Import selected memiavl modules into FlatKV",
		Long: strings.TrimSpace(`Import selected memiavl modules into FlatKV.

This is an offline test-seeding and pre-migration recovery tool, not the
production migration or state-sync path. Use it to construct FlatKV test data
from a memiavl-only store or to rebuild FlatKV from a pre-migration backup.
It does not write migration metadata. Stop seid before running it; the import
fails if the memiavl database is locked by a running node.

WARNING: this restore-style import resets the FlatKV directory before loading
the imported rows. If FlatKV already has committed data, the command refuses to
run unless --force is supplied.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, _ := cmd.Flags().GetString("home")
			dataDir, _ := cmd.Flags().GetString("data-dir")
			modules, _ := cmd.Flags().GetStringSlice("modules")
			height, _ := cmd.Flags().GetInt64("height")
			force, _ := cmd.Flags().GetBool("force")

			resolvedHome, err := resolveSeiHome(homeDir, dataDir)
			if err != nil {
				return err
			}
			modules, err = normalizeImportModules(modules)
			if err != nil {
				return err
			}
			if height < 0 {
				return fmt.Errorf("height %d out of range", height)
			}

			return importMemiavlModulesToFlatKV(cmd.Context(), resolvedHome, modules, height, force)
		},
	}
	cmd.Flags().String("home", "", "Sei home directory. Defaults to $HOME/.sei")
	cmd.Flags().String("data-dir", "", "Sei data directory or home directory. If the basename is data, its parent is used as home")
	cmd.Flags().StringSlice("modules", []string{keys.EVMStoreKey}, "Comma-separated module names to import. Initial production scope supports only evm")
	cmd.Flags().Int64("height", 0, "memiavl version to import. 0 means latest")
	cmd.Flags().Bool("force", false, "Overwrite existing committed FlatKV data")
	return cmd
}

func resolveSeiHome(homeDir, dataDir string) (string, error) {
	if homeDir != "" {
		return filepath.Abs(homeDir)
	}
	if dataDir != "" {
		clean := filepath.Clean(dataDir)
		if filepath.Base(clean) == "data" {
			return filepath.Abs(filepath.Dir(clean))
		}
		return filepath.Abs(clean)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home: %w", err)
	}
	return filepath.Join(home, ".sei"), nil
}

func normalizeImportModules(modules []string) ([]string, error) {
	if len(modules) == 0 {
		modules = []string{keys.EVMStoreKey}
	}
	seen := make(map[string]struct{}, len(modules))
	normalized := make([]string, 0, len(modules))
	for _, module := range modules {
		for _, part := range strings.Split(module, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if name != keys.EVMStoreKey {
				return nil, fmt.Errorf("module %q is not supported yet; initial import scope is evm-only", name)
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one module must be specified")
	}
	return normalized, nil
}

// importerErr surfaces any pipeline error the FlatKV importer's worker
// goroutines have already recorded, so the import loop can fail-fast
// between exporter reads instead of waiting until Close. The anonymous
// interface assertion (rather than a concrete *flatkv.KVImporter type
// switch) lets any future Importer impl opt into mid-stream error
// reporting just by adding Err() error to its method set, without
// touching this helper.
func importerErr(importer sctypes.Importer) error {
	if e, ok := importer.(interface{ Err() error }); ok {
		return e.Err()
	}
	return nil
}

// emitPairs forwards translator output to the FlatKV importer, returning the
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

func importMemiavlModulesToFlatKV(ctx context.Context, homeDir string, modules []string, height int64, force bool) (err error) {
	cosmosDir := utils.GetCosmosSCStorePath(homeDir)
	if height > math.MaxUint32 {
		return fmt.Errorf("height %d out of range", height)
	}

	// Exclude a live memiavl writer for the entire check-and-import sequence.
	// Without this lock, the node could commit after memiavlLatest is checked
	// but before FlatKV is finalized, leaving the stores at different heights
	// and causing reconciliation to roll memiavl back on the next startup.
	memLock, err := memiavl.LockFile(filepath.Join(cosmosDir, memiavl.LockFileName))
	if err != nil {
		return fmt.Errorf("failed to lock memiavl database for FlatKV import (is seid still running?): %w", err)
	}
	defer func() {
		if unlockErr := memLock.Unlock(); unlockErr != nil && err == nil {
			err = fmt.Errorf("failed to unlock memiavl database after FlatKV import: %w", unlockErr)
		}
	}()

	memiavlLatest, err := memiavl.GetLatestVersion(cosmosDir)
	if err != nil {
		return fmt.Errorf("failed to resolve latest memiavl version from %s: %w", cosmosDir, err)
	}
	if height == 0 {
		height = memiavlLatest
	}
	if height <= 0 {
		return fmt.Errorf("height must be positive after resolution, got %d", height)
	}
	// Refuse mismatched heights. If we wrote FlatKV at H < memiavlLatest,
	// the next GIGA_STORAGE startup would call
	// CompositeCommitStore.reconcileVersions (see
	// sei-db/state_db/sc/composite/store.go) and silently roll memiavl
	// back to H, truncating every cosmos block in (H, memiavlLatest].
	// H > memiavlLatest is unreachable in practice (the memiavl exporter
	// would error a few lines below) but caught here for a clearer
	// message. Operators who genuinely want a non-latest H must first
	// roll memiavl back to H themselves; this CLI deliberately does NOT
	// roll memiavl back on their behalf because "import" is a one-way,
	// abortable operation and should never be a hidden gateway into a
	// destructive cosmos rollback.
	if height < memiavlLatest {
		return fmt.Errorf(
			"refusing to import FlatKV at height %d while memiavl latest is %d: "+
				"a subsequent GIGA_STORAGE startup would call CompositeCommitStore.reconcileVersions "+
				"and silently roll memiavl back to %d, truncating cosmos blocks (%d, %d]; "+
				"roll memiavl back to %d first, then re-run this import",
			height, memiavlLatest, height, height, memiavlLatest, height)
	}
	if height > memiavlLatest {
		return fmt.Errorf(
			"refusing to import FlatKV at height %d which is ahead of memiavl latest %d",
			height, memiavlLatest)
	}

	moduleSet := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		moduleSet[module] = struct{}{}
	}

	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = utils.GetFlatKVPath(homeDir)
	store, err := flatkv.NewCommitStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create FlatKV store: %w", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.LoadVersion(0, false); err != nil {
		return fmt.Errorf("failed to open FlatKV store: %w", err)
	}

	if store.Version() > 0 {
		if !force {
			return fmt.Errorf("FlatKV store at %s already has committed version %d; rerun with --force to overwrite it",
				cfg.DataDir, store.Version())
		}
		fmt.Printf("WARNING: --force set; overwriting existing FlatKV store at %s (current version %d)\n",
			cfg.DataDir, store.Version())
	}

	exporter, err := memiavl.NewMultiTreeExporter(cosmosDir, uint32(height), false) //nolint:gosec // height range checked above
	if err != nil {
		return fmt.Errorf("failed to open memiavl exporter at height %d: %w", height, err)
	}
	defer func() { _ = exporter.Close() }()

	importer, err := store.Importer(height)
	if err != nil {
		return fmt.Errorf("failed to create FlatKV importer at height %d: %w", height, err)
	}
	// On the failure path we must NOT finalize: KVImporter.Close otherwise
	// commits whatever pairs were already buffered, leaving FlatKV at the
	// target version with only a partial copy of the source state. Route
	// errors through Abort instead, which records the failure on the
	// importer and then drains workers without writing a snapshot. On the
	// success path the explicit Close below has already run, so the
	// deferred Close here is just an idempotent safety net.
	defer func() {
		if err != nil {
			if kvi, ok := importer.(*flatkv.KVImporter); ok {
				_ = kvi.Abort(err)
			}
			// err path: do NOT call Close, which would finalize the partial
			// import (see KVImporter.Close docstring). If the type assertion
			// fails (future Importer impl), leave the pipeline to GC -- a
			// leak strictly beats silently committing a half-imported snapshot.
			return
		}
		_ = importer.Close()
	}()

	translator := flatkv.NewImportTranslator(height)
	batch := &proto.NamedChangeSet{
		Changeset: proto.ChangeSet{Pairs: make([]*proto.KVPair, 0, translatorBatchSize)},
	}
	var written int64
	flush := func() error {
		if len(batch.Changeset.Pairs) == 0 {
			return nil
		}
		pairs, err := translator.Translate(batch)
		if err != nil {
			return fmt.Errorf("translate batch (module=%s): %w", batch.Name, err)
		}
		written += emitPairs(importer, pairs, height)
		batch.Changeset.Pairs = batch.Changeset.Pairs[:0]
		return nil
	}

	// acceptCurrent caches whether the current module (batch.Name) is in
	// moduleSet so the per-pair SnapshotNode arm doesn't repeat the map
	// lookup for every key emitted by the exporter. It's recomputed once
	// per module switch in the `case string:` arm below.
	var acceptCurrent bool
	var imported int64
	moduleCounts := make(map[string]int64, len(modules))
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("import interrupted: %w", err)
		}
		if err := importerErr(importer); err != nil {
			return fmt.Errorf("FlatKV import failed: %w", err)
		}

		item, err := exporter.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				break
			}
			return fmt.Errorf("failed to export memiavl data: %w", err)
		}
		switch v := item.(type) {
		case string:
			if err := flush(); err != nil {
				return err
			}
			batch.Name = v
			_, acceptCurrent = moduleSet[v]
			if acceptCurrent {
				// AddModule takes the source module name (here the memiavl
				// module being read), not the destination store name. On
				// *flatkv.KVImporter this is currently a no-op, but
				// telemetry-/log-bearing implementations downstream will
				// attribute the import to batch.Name rather than
				// hard-coding it to "flatkv".
				if err := importer.AddModule(v); err != nil {
					return fmt.Errorf("failed to add import module %q: %w", v, err)
				}
			}
		case *sctypes.SnapshotNode:
			// EVM-only choke point. normalizeImportModules already rejects
			// non-EVM module names at the CLI boundary, so today this skip
			// is defense-in-depth. If a future expansion adds another
			// module to the allow-list, this `continue` is what keeps that
			// module's pairs out of the importer -- the flatkv store does
			// not have a routing path for non-EVM physical keys yet, and
			// silently accepting them would land them in the legacyDB
			// bucket. Any allow-list change MUST be paired with a flatkv
			// routePhysicalKey extension; otherwise leave this skip alone.
			if !acceptCurrent {
				continue
			}
			if v == nil || v.Height != 0 || v.Value == nil {
				continue
			}
			batch.Changeset.Pairs = append(batch.Changeset.Pairs, &proto.KVPair{
				Key:   v.Key,
				Value: v.Value,
			})
			imported++
			moduleCounts[batch.Name]++
			if len(batch.Changeset.Pairs) >= translatorBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected export item type %T", item)
		}
	}
	if err := flush(); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("import interrupted: %w", err)
	}
	if err := importerErr(importer); err != nil {
		return fmt.Errorf("FlatKV import failed: %w", err)
	}

	written += emitPairs(importer, translator.Finalize(), height)

	if err := importer.Close(); err != nil {
		return fmt.Errorf("failed to finalize FlatKV import: %w", err)
	}
	fmt.Printf("Imported %d memiavl key/value pairs into %d FlatKV rows from modules %v at height %d (per-module: %v)\n",
		imported, written, modules, height, moduleCounts)
	return nil
}

// MemiavlLatestVersionCmd is the read-only companion to ImportFlatKVFromMemiavlCmd:
// it reports the latest committed memiavl version of a stopped node so an
// orchestration script can pick a single import height across a multi-validator
// cluster. Lives in this file (rather than a standalone *_cmd.go) because it
// shares resolveSeiHome with the import command and exists solely to support
// that workflow -- see integration_test/contracts/import_flatkv_evm_cluster.sh
// for the call site, which reads each validator's version after pkill, picks
// the minimum, and rolls back any node that committed extra blocks before
// running the offline import.
func MemiavlLatestVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memiavl-latest-version",
		Short: "Print the latest memiavl version of a stopped node",
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, _ := cmd.Flags().GetString("home")
			dataDir, _ := cmd.Flags().GetString("data-dir")

			resolvedHome, err := resolveSeiHome(homeDir, dataDir)
			if err != nil {
				return err
			}

			version, err := memiavl.GetLatestVersion(utils.GetCosmosSCStorePath(resolvedHome))
			if err != nil {
				return fmt.Errorf("failed to resolve latest memiavl version: %w", err)
			}
			fmt.Println(version)
			return nil
		},
	}
	cmd.Flags().String("home", "", "Sei home directory. Defaults to $HOME/.sei")
	cmd.Flags().String("data-dir", "", "Sei data directory or home directory. If the basename is data, its parent is used as home")
	return cmd
}
