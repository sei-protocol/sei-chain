package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/snapshots"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/app"
)

// SnapshotCmd creates a new command to trigger snapshot creation
func SnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Create a snapshot using the snapshot manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			homeDir := config.RootDir

			// Get height from flag
			height, err := cmd.Flags().GetInt64("height")
			if err != nil {
				return fmt.Errorf("failed to get height: %w", err)
			}
			if height <= 0 {
				return fmt.Errorf("height must be greater than 0")
			}

			// Get snapshot directory from flag
			snapshotDir, err := cmd.Flags().GetString("snapshot-dir")
			if err != nil {
				return fmt.Errorf("failed to get snapshot directory: %w", err)
			}
			if snapshotDir == "" {
				snapshotDir = filepath.Join(homeDir, "data", "snapshots")
			}

			snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
			if err != nil {
				panic(err)
			}
			snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
			if err != nil {
				panic(err)
			}

			// Create logger
			logger := log.NewTMLogger(os.Stdout)

			// Get app options from server context
			appOpts := serverCtx.Viper

			// Set SeiDB KeepRecent to 0 for snapshot creation to avoid keeping old versions
			appOpts.Set(app.FlagSSKeepRecent, 0)

			// Initialize app with correct options
			app := app.New(
				logger,
				dbm.NewMemDB(),
				nil,
				true,
				map[int64]bool{},
				homeDir,
				0,
				true,
				config,
				app.MakeEncodingConfig(),
				app.GetWasmEnabledProposals(),
				appOpts,
				[]wasm.Option{},
				app.EmptyAppOptions,
				baseapp.SetSnapshotStore(snapshotStore),
				baseapp.SetPruning(storetypes.NewPruningOptionsFromString(storetypes.PruningOptionNothing)),
				baseapp.SetSnapshotKeepRecent(0),
			)

			// Set chain ID from flag
			app.ChainID = cast.ToString(appOpts.Get(flags.FlagChainID))

			// Create snapshot
			fmt.Printf("Creating snapshot at height %d...\n", height)

			// Create snapshot directory if it doesn't exist
			if err := os.MkdirAll(snapshotDir, 0750); err != nil {
				return fmt.Errorf("failed to create snapshot directory: %w", err)
			}

			// Create snapshot using snapshot manager
			snapshotManager := app.SnapshotManager()
			if snapshotManager == nil {
				return fmt.Errorf("snapshot manager is not initialized")
			}

			// Create snapshot
			_, err = snapshotManager.Create(uint64(height))
			if err != nil {
				return fmt.Errorf("failed to create snapshot: %w", err)
			}

			fmt.Printf("Successfully created snapshot at height %d in directory %s\n", height, snapshotDir)
			return nil
		},
	}

	// Add flags
	cmd.Flags().Int64("height", 0, "Height at which to create the snapshot (required)")
	cmd.Flags().String("snapshot-dir", "", "Directory to store the snapshot (default: <home>/data/snapshots)")
	cmd.Flags().String(flags.FlagChainID, "", "The network chain ID")
	_ = cmd.MarkFlagRequired("height")

	return cmd
}
