package cmd

import (

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// runRestoreSnapshotCmd is the command runner for RestoreSnapshotCmd
func runRestoreSnapshotCmd(cmd *cobra.Command, args []string) error {
	snapshotDir, err := cmd.Flags().GetString("snapshot-dir")
	if err != nil {
		return err
	}

	homeDir, err := cmd.Flags().GetString("home")
	if err != nil {
		return err
	}

	// Get server context to access app options
	serverCtx := server.GetServerContextFromCmd(cmd)
	viper := serverCtx.Viper

	// Access SeiDB SC (State Commit) configurations
	scEnabled := cast.ToBool(viper.Get("state-commit.sc-enable"))
	scDirectory := cast.ToString(viper.Get("state-commit.sc-directory"))
	scAsyncCommitBuffer := cast.ToInt(viper.Get("state-commit.sc-async-commit-buffer"))
	scSnapshotInterval := cast.ToUint32(viper.Get("state-commit.sc-snapshot-interval"))

	// Access SeiDB SS (State Store) configurations
	ssEnabled := cast.ToBool(viper.Get("state-store.ss-enable"))
	ssBackend := cast.ToString(viper.Get("state-store.ss-backend"))
	ssKeepRecent := cast.ToInt(viper.Get("state-store.ss-keep-recent"))
	ssPruneInterval := cast.ToInt(viper.Get("state-store.ss-prune-interval"))

	// Print configurations for debugging
	cmd.Printf("SeiDB SC Configurations:\n")
	cmd.Printf("  Enabled: %v\n", scEnabled)
	cmd.Printf("  Directory: %s\n", scDirectory)
	cmd.Printf("  Async Commit Buffer: %d\n", scAsyncCommitBuffer)
	cmd.Printf("  Snapshot Interval: %d\n", scSnapshotInterval)

	cmd.Printf("\nSeiDB SS Configurations:\n")
	cmd.Printf("  Enabled: %v\n", ssEnabled)
	cmd.Printf("  Backend: %s\n", ssBackend)
	cmd.Printf("  Keep Recent: %d\n", ssKeepRecent)
	cmd.Printf("  Prune Interval: %d\n", ssPruneInterval)

	cmd.Printf("\nRestore Parameters:\n")
	cmd.Printf("  Snapshot Directory: %s\n", snapshotDir)
	cmd.Printf("  Home Directory: %s\n", homeDir)

	return nil
}

// RestoreSnapshotCmd returns a new cobra command for restoring a snapshot
func RestoreSnapshotCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-snapshot [--snapshot-dir <directory>]",
		Short: "Restore application state from a local state sync snapshot",
		Long:  `Restore application state from a local state sync snapshot. This command is a placeholder and not yet implemented.`,
		Args:  cobra.ExactArgs(0),
		RunE:  runRestoreSnapshotCmd,
	}

	cmd.Flags().String("snapshot-dir", "", "Directory containing the snapshot to restore")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")

	return cmd
}
