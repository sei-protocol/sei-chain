package operations

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/spf13/cobra"
)

// FlatKVRecordSeededVersionCmd is the seidb subcommand that records the version a FlatKV store was seeded at,
// for a store seeded before the seed recorded it. The node must be stopped.
func FlatKVRecordSeededVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flatkv-record-seeded-version",
		Short: "Record the version a FlatKV store was seeded at",
		Long: "Validates --version against the history the stopped node's FlatKV store holds and records it as the " +
			"height the store was seeded at: one below the first block FlatKV committed. Heights at or below it " +
			"are then served from memIAVL alone.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, _ := cmd.Flags().GetString("home")
			dataDir, _ := cmd.Flags().GetString("data-dir")
			version, _ := cmd.Flags().GetInt64("version")
			resolvedHome, err := resolveSeiHome(homeDir, dataDir)
			if err != nil {
				return err
			}
			if version <= 0 {
				return fmt.Errorf("--version must be a positive height, got %d", version)
			}
			return recordFlatKVSeededVersion(cmd.Context(), resolvedHome, version)
		},
	}
	cmd.Flags().String("home", "", "Sei home directory. Defaults to $HOME/.sei")
	cmd.Flags().String("data-dir", "", "Sei data directory or home directory. If the basename is data, its parent is used as home")
	cmd.Flags().Int64("version", 0, "Height the FlatKV store was seeded at")
	return cmd
}

func recordFlatKVSeededVersion(ctx context.Context, homeDir string, version int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = utils.GetFlatKVPath(homeDir)
	stateWAL, err := flatkv.OpenStateWAL(cfg)
	if err != nil {
		return fmt.Errorf("failed to open FlatKV state WAL: %w", err)
	}
	store, err := flatkv.NewCommitStore(ctx, cfg, stateWAL)
	if err != nil {
		_ = stateWAL.Close()
		return fmt.Errorf("failed to create FlatKV store: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.LoadLatest(); err != nil {
		return fmt.Errorf("failed to open FlatKV store: %w", err)
	}
	if err := store.RecordSeededVersion(version); err != nil {
		return err
	}
	fmt.Printf("Recorded FlatKV seeded version %d in %s\n", version, cfg.DataDir)
	return nil
}
