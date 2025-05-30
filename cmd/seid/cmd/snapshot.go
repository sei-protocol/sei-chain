package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/server"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/libs/log"
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

			// Get snapshot directory from flag
			snapshotDir, err := cmd.Flags().GetString("snapshot-dir")
			if err != nil {
				return fmt.Errorf("failed to get snapshot directory: %w", err)
			}
			if snapshotDir == "" {
				snapshotDir = filepath.Join(homeDir, "data", "snapshots")
			}

			// Create logger
			logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

			// Initialize app
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
				app.TestAppOpts{},
				[]wasm.Option{},
				[]aclkeeper.Option{},
				app.EmptyAppOptions,
			)

			// Load version at specified height
			if err := app.LoadVersion(height); err != nil {
				return fmt.Errorf("failed to load version at height %d: %w", height, err)
			}

			// Create snapshot
			fmt.Printf("Creating snapshot at height %d...\n", height)

			// Create snapshot directory if it doesn't exist
			if err := os.MkdirAll(snapshotDir, 0755); err != nil {
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
	cmd.Flags().Int64("height", 0, "Height at which to create the snapshot")
	cmd.Flags().String("snapshot-dir", "", "Directory to store the snapshot (default: <home>/data/snapshots)")
	cmd.MarkFlagRequired("height")

	return cmd
}
