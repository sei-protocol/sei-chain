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

// importBatchSize bounds how many memiavl key/value pairs we hand to a single
// flatkv.ImportTranslator.Translate call. Batching amortizes the per-call
// classifyAndPrefix map allocations across many keys without growing
// ImportTranslator's account-buffer memory beyond what an unbatched stream
// would already need.
const importBatchSize = 2048

// ImportFlatKVFromMemiavlCmd imports selected memiavl modules into FlatKV.
//
// Initial production scope is intentionally narrow: only the evm module is
// accepted. Non-EVM modules remain in memiavl and are not copied into FlatKV.
// Importing resets FlatKV and replaces it with the selected memiavl data; the
// CLI refuses to run over existing FlatKV data unless --force is supplied.
func ImportFlatKVFromMemiavlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-flatkv-from-memiavl",
		Short: "Import selected memiavl modules into FlatKV",
		Long: strings.TrimSpace(`Import selected memiavl modules into FlatKV.

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

func importerErr(importer sctypes.Importer) error {
	if importer == nil {
		return nil
	}
	return importer.Err()
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

func importMemiavlModulesToFlatKV(ctx context.Context, homeDir string, modules []string, height int64, force bool) error {
	cosmosDir := utils.GetCosmosSCStorePath(homeDir)
	if height == 0 {
		latest, err := memiavl.GetLatestVersion(cosmosDir)
		if err != nil {
			return fmt.Errorf("failed to resolve latest memiavl version from %s: %w", cosmosDir, err)
		}
		height = latest
	}
	if height <= 0 {
		return fmt.Errorf("height must be positive after resolution, got %d", height)
	}
	if height > math.MaxUint32 {
		return fmt.Errorf("height %d out of range", height)
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
	defer func() { _ = importer.Close() }()

	translator := flatkv.NewImportTranslator(height)
	batch := &proto.NamedChangeSet{
		Changeset: proto.ChangeSet{Pairs: make([]*proto.KVPair, 0, importBatchSize)},
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

	var currentModule string
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
			currentModule = v
			batch.Name = currentModule
			if _, ok := moduleSet[currentModule]; ok {
				if err := importer.AddModule(keys.FlatKVStoreKey); err != nil {
					return fmt.Errorf("failed to add FlatKV import module: %w", err)
				}
			}
		case *sctypes.SnapshotNode:
			if _, ok := moduleSet[currentModule]; !ok {
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
			moduleCounts[currentModule]++
			if len(batch.Changeset.Pairs) >= importBatchSize {
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
