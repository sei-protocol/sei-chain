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

			// Load latest version
			if err := app.LoadLatestVersion(); err != nil {
				return fmt.Errorf("failed to load latest version: %w", err)
			}

			// Get current height
			height := app.LastBlockHeight()
			if height == 0 {
				return fmt.Errorf("no blocks found")
			}

			// Create snapshot
			fmt.Printf("Creating snapshot at height %d...\n", height)

			// Create snapshot directory if it doesn't exist
			snapshotDir := filepath.Join(homeDir, "data", "snapshots")
			if err := os.MkdirAll(snapshotDir, 0755); err != nil {
				return fmt.Errorf("failed to create snapshot directory: %w", err)
			}

			// Create snapshot using snapshot manager
			snapshotManager := app.SnapshotManager()
			if snapshotManager == nil {
				return fmt.Errorf("snapshot manager is not initialized")
			}

			// Create snapshot
			_, err := snapshotManager.Create(uint64(height))
			if err != nil {
				return fmt.Errorf("failed to create snapshot: %w", err)
			}

			fmt.Printf("Successfully created snapshot at height %d\n", height)
			return nil
		},
	}

	return cmd
}
