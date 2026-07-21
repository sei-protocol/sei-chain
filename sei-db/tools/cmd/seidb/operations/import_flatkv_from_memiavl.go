package operations

import (
	"context"
	"encoding/binary"
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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
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

// importAllModules is the sentinel accepted by --modules that selects every
// module emitted by the memiavl exporter (a full memiavl -> FlatKV
// conversion, as a completed bank migration would leave the store).
const importAllModules = "all"

// markMigratedVersions maps the accepted --mark-as-migrated values to the
// migration version persisted after a successful import.
var markMigratedVersions = map[string]uint64{
	keys.EVMStoreKey: migration.Version1_MigrateEVM,
	importAllModules: migration.Version3_FlatKVOnly,
}

// ImportFlatKVFromMemiavlCmd imports selected memiavl modules into FlatKV.
//
// Two scopes are supported: the evm module alone (the original production
// scope), or --modules all for a full memiavl -> FlatKV conversion. Arbitrary
// module subsets are rejected: a partial non-EVM import corresponds to no
// valid migration stage and would leave the store in a state DeriveWriteMode
// cannot classify. Importing resets FlatKV and replaces it with the selected
// memiavl data; the CLI refuses to run over existing FlatKV data unless
// --force is supplied.
func ImportFlatKVFromMemiavlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-flatkv-from-memiavl",
		Short: "Import selected memiavl modules into FlatKV",
		Long: strings.TrimSpace(`Import selected memiavl modules into FlatKV.

Supported scopes: the evm module alone (default), or --modules all to convert
every memiavl module into FlatKV.

With --mark-as-migrated, a successful import also persists the matching
migration version into FlatKV's migration store (evm -> version 1, all ->
version 3) so a subsequent startup derives the corresponding write mode.

WARNING: this restore-style import resets the FlatKV directory before loading
the imported rows. If FlatKV already has committed data, the command refuses to
run unless --force is supplied.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, _ := cmd.Flags().GetString("home")
			dataDir, _ := cmd.Flags().GetString("data-dir")
			modules, _ := cmd.Flags().GetStringSlice("modules")
			height, _ := cmd.Flags().GetInt64("height")
			force, _ := cmd.Flags().GetBool("force")
			markAsMigrated, _ := cmd.Flags().GetString("mark-as-migrated")

			resolvedHome, err := resolveSeiHome(homeDir, dataDir)
			if err != nil {
				return err
			}
			modules, importAll, err := normalizeImportModules(modules)
			if err != nil {
				return err
			}
			markVersion, err := resolveMarkAsMigrated(markAsMigrated, importAll)
			if err != nil {
				return err
			}
			if height < 0 {
				return fmt.Errorf("height %d out of range", height)
			}

			return importMemiavlModulesToFlatKV(cmd.Context(), resolvedHome, importScope{
				modules:     modules,
				importAll:   importAll,
				height:      height,
				force:       force,
				markVersion: markVersion,
			})
		},
	}
	cmd.Flags().String("home", "", "Sei home directory. Defaults to $HOME/.sei")
	cmd.Flags().String("data-dir", "", "Sei data directory or home directory. If the basename is data, its parent is used as home")
	cmd.Flags().StringSlice("modules", []string{keys.EVMStoreKey}, "Modules to import: evm (default) or all")
	cmd.Flags().Int64("height", 0, "memiavl version to import. 0 means latest")
	cmd.Flags().Bool("force", false, "Overwrite existing committed FlatKV data")
	cmd.Flags().String("mark-as-migrated", "", "After a successful import, persist the matching migration version into FlatKV: evm (version 1) or all (version 3). Must match the import scope")
	return cmd
}

// importScope bundles the resolved CLI inputs for importMemiavlModulesToFlatKV.
type importScope struct {
	modules   []string
	importAll bool
	height    int64
	force     bool
	// markVersion, when non-zero, is the migration version to persist into
	// FlatKV's migration store as part of the import.
	markVersion uint64
}

// resolveMarkAsMigrated validates --mark-as-migrated against the import scope
// and returns the migration version to persist (0 = none).
func resolveMarkAsMigrated(value string, importAll bool) (uint64, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, nil
	}
	version, ok := markMigratedVersions[value]
	if !ok {
		return 0, fmt.Errorf("--mark-as-migrated %q is not supported; expected %q or %q", value, keys.EVMStoreKey, importAllModules)
	}
	if importAll != (value == importAllModules) {
		return 0, fmt.Errorf("--mark-as-migrated %q does not match the import scope; use --mark-as-migrated=evm with --modules evm and --mark-as-migrated=all with --modules all", value)
	}
	return version, nil
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

// normalizeImportModules parses --modules into either the evm-only scope or
// the all-modules scope (importAll=true, modules=nil). Arbitrary subsets are
// rejected: a partial non-EVM import corresponds to no valid migration stage.
func normalizeImportModules(modules []string) ([]string, bool, error) {
	if len(modules) == 0 {
		modules = []string{keys.EVMStoreKey}
	}
	seen := make(map[string]struct{}, len(modules))
	normalized := make([]string, 0, len(modules))
	for _, module := range modules {
		for _, part := range strings.Split(module, ",") {
			name := strings.TrimSpace(strings.ToLower(part))
			if name == "" {
				continue
			}
			if name != keys.EVMStoreKey && name != importAllModules {
				return nil, false, fmt.Errorf(
					"module %q is not supported; use %q for the evm module or %q for a full conversion",
					name, keys.EVMStoreKey, importAllModules)
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
	}
	if len(normalized) == 0 {
		return nil, false, errors.New("at least one module must be specified")
	}
	if len(normalized) > 1 {
		return nil, false, fmt.Errorf("--modules %v is ambiguous; specify either %q or %q", normalized, keys.EVMStoreKey, importAllModules)
	}
	if normalized[0] == importAllModules {
		return nil, true, nil
	}
	return normalized, false, nil
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

func importMemiavlModulesToFlatKV(ctx context.Context, homeDir string, scope importScope) (err error) {
	modules, height, force := scope.modules, scope.height, scope.force
	cosmosDir := utils.GetCosmosSCStorePath(homeDir)
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
	if height > math.MaxUint32 {
		return fmt.Errorf("height %d out of range", height)
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
			if scope.importAll {
				acceptCurrent = true
			} else {
				_, acceptCurrent = moduleSet[v]
			}
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
			// Scope choke point. In the evm-only scope this skip keeps
			// non-EVM pairs out of the importer; in the all-modules scope
			// every module is accepted. Non-EVM pairs are handled by the
			// same path a live FlatKVOnly commit uses: classifyAndPrefix
			// (inside ImportTranslator) prefixes them "<module>/" into the
			// misc bucket and routePhysicalKey routes them to miscDB.
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

	// Persist the migration version through the import stream itself (rather
	// than a post-import ApplyChangeSets+Commit, which would advance the
	// FlatKV version past the memiavl height). The key rides the same
	// translator path a live migration-completion block uses — a misc-bucket
	// pair under the "migration/" module prefix — so it participates in the
	// imported store's LtHash exactly like it would on a live node.
	if scope.markVersion != 0 {
		versionBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(versionBytes, scope.markVersion)
		metaPairs, err := translator.Translate(&proto.NamedChangeSet{
			Name: migration.MigrationStore,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(migration.MigrationVersionKey), Value: versionBytes},
			}},
		})
		if err != nil {
			return fmt.Errorf("translate migration metadata: %w", err)
		}
		written += emitPairs(importer, metaPairs, height)
	}

	written += emitPairs(importer, translator.Finalize(), height)

	if err := importer.Close(); err != nil {
		return fmt.Errorf("failed to finalize FlatKV import: %w", err)
	}
	scopeLabel := fmt.Sprintf("%v", modules)
	if scope.importAll {
		scopeLabel = importAllModules
	}
	fmt.Printf("Imported %d memiavl key/value pairs into %d FlatKV rows from modules %s at height %d (per-module: %v)\n",
		imported, written, scopeLabel, height, moduleCounts)
	if scope.markVersion != 0 {
		fmt.Printf("Marked FlatKV migration version %d in the %q store\n", scope.markVersion, migration.MigrationStore)
	}
	if scope.importAll && scope.markVersion == migration.Version3_FlatKVOnly {
		fmt.Println("NOTE: for a flatkv_only node the memiavl directory is no longer read; it can be removed to reclaim space")
	}
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
