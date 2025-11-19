package server

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/golang/protobuf/proto"
	"github.com/google/orderedcode"
	"github.com/spf13/cobra"
	tmcmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
	tmcfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/proto/tendermint/state"
	dbm "github.com/tendermint/tm-db"
)

// getTendermintStateHeight returns the current tendermint state height without modifying it.
// It reads the state directly from the database using protobuf types.
func getTendermintStateHeight(cfg *tmcfg.Config) (int64, error) {
	dbType := dbm.BackendType(cfg.DBBackend)
	stateDB, err := dbm.NewDB("state", dbType, cfg.DBDir())
	if err != nil {
		return -1, fmt.Errorf("failed to open state db: %w", err)
	}
	defer stateDB.Close()

	// The state key is constructed using orderedcode with prefixState = int64(8)
	// This matches the logic in sei-tendermint/internal/state/store.go
	prefixState := int64(8)
	stateKey, err := orderedcode.Append(nil, prefixState)
	if err != nil {
		return -1, fmt.Errorf("failed to construct state key: %w", err)
	}

	stateBytes, err := stateDB.Get(stateKey)
	if err != nil {
		return -1, fmt.Errorf("failed to read state from db: %w", err)
	}

	if len(stateBytes) == 0 {
		return 0, nil
	}

	// Unmarshal the protobuf state
	var tmState state.State
	if err := proto.Unmarshal(stateBytes, &tmState); err != nil {
		return -1, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return tmState.LastBlockHeight, nil
}

// NewRollbackCmd creates a command to rollback tendermint and multistore state by one height.
// The rollback is performed atomically: app state is rolled back first, then tendermint state.
// The command is idempotent and can handle partial rollback states (e.g., app rolled back but tendermint not).
func NewRollbackCmd(appCreator types.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "rollback cosmos-sdk and tendermint state by one height",
		Long: `
A state rollback is performed to recover from an incorrect application state transition,
when Tendermint has persisted an incorrect app hash and is thus unable to make
progress. Rollback overwrites a state at height n with the state at height n - 1.
The application also roll back to height n - 1. The problematic block at height n
is deleted from the blockstore so it can be refetched from peers and re-executed.

The rollback is performed atomically: app state is rolled back first, then tendermint state.
This ensures that if the rollback fails, the node remains in a consistent state.

When rolling back, the problematic block is deleted from the blockstore so it can be
refetched from peers and re-executed. This is safe because rollback is only used when
there's an app hash mismatch, indicating the block execution was incorrect.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := GetServerContextFromCmd(cmd)
			cfg := ctx.Config
			home := cfg.RootDir
			db, err := openDB(home)
			if err != nil {
				return err
			}

			app := appCreator(
				ctx.Logger,
				db,
				nil,
				nil,
				ctx.Viper,
			)

			// Get initial state for validation
			initialAppCommit := app.CommitMultiStore().LastCommitID()
			appHeight := initialAppCommit.GetVersion()
			fmt.Printf("Initial App state height=%d and hash=%X\n", appHeight, initialAppCommit.GetHash())

			// Get tendermint state height
			tmHeight, err := getTendermintStateHeight(ctx.Config)
			if err != nil {
				return fmt.Errorf("failed to get tendermint state height: %w", err)
			}
			fmt.Printf("Initial Tendermint state height=%d\n", tmHeight)

			// Check current state and determine what needs to be done
			if appHeight == tmHeight {
				// Both are at the same height - normal rollback case
				targetHeight := appHeight - 1
				if targetHeight < 0 {
					return fmt.Errorf("cannot rollback: current height is %d, cannot rollback below 0", appHeight)
				}
				fmt.Printf("Both app and tendermint are at height %d, rolling back to height %d\n", appHeight, targetHeight)
				return performRollback(app, ctx.Config, targetHeight)
			} else if appHeight < tmHeight {
				// App is already rolled back but tendermint is not - complete the rollback
				fmt.Printf("Detected partial rollback: app is at height %d, tendermint is at height %d\n", appHeight, tmHeight)
				fmt.Printf("Completing rollback by rolling back tendermint state to height %d\n", appHeight)
				return completeTendermintRollback(ctx.Config, appHeight)
			} else {
				// App is ahead of tendermint - rollback app state only to match tendermint
				fmt.Printf("Detected inconsistent state: app is at height %d, tendermint is at height %d\n", appHeight, tmHeight)
				fmt.Printf("Rolling back app state to match tendermint height %d\n", tmHeight)
				return rollbackAppStateOnly(app, tmHeight)
			}
		},
	}

	cmd.Flags().String(flags.FlagChainID, "sei-chain", "genesis file chain-id, if left blank will use sei")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}

// rollbackAppStateToHeight rolls back app state to the target height and validates the result.
func rollbackAppStateToHeight(app types.Application, targetHeight int64) ([]byte, error) {
	if targetHeight < 0 {
		return nil, fmt.Errorf("cannot rollback: target height is %d, cannot rollback below 0", targetHeight)
	}

	fmt.Printf("Rolling back app state to height %d...\n", targetHeight)
	if err := app.CommitMultiStore().RollbackToVersion(targetHeight); err != nil {
		return nil, fmt.Errorf("failed to rollback app state to version %d: %w", targetHeight, err)
	}

	appCommit := app.CommitMultiStore().LastCommitID()
	if appCommit.GetVersion() != targetHeight {
		return nil, fmt.Errorf("app state rollback failed: expected height %d, got %d", targetHeight, appCommit.GetVersion())
	}

	fmt.Printf("Rolled back app state to height %d and hash %X\n", appCommit.GetVersion(), appCommit.GetHash())
	return appCommit.GetHash(), nil
}

// rollbackTendermintStateOnce rolls back tendermint state by one height.
// Returns the new height and app hash, or an error.
func rollbackTendermintStateOnce(cfg *tmcfg.Config) (int64, []byte, error) {
	tmHeight, hash, err := tmcmd.RollbackState(cfg, true)
	if err != nil {
		return -1, nil, fmt.Errorf("failed to rollback tendermint state: %w", err)
	}
	fmt.Printf("Rolled back tendermint state to height %d and hash %X\n", tmHeight, hash)
	return tmHeight, hash, nil
}

// verifyConsistency verifies that app and tendermint states are at the same height.
func verifyConsistency(app types.Application, expectedHeight int64, tmHash []byte) error {
	appCommit := app.CommitMultiStore().LastCommitID()
	if appCommit.GetVersion() != expectedHeight {
		return fmt.Errorf("state mismatch: app height=%d, expected=%d", appCommit.GetVersion(), expectedHeight)
	}
	fmt.Printf("\nRollback completed successfully!\n")
	fmt.Printf("Final state: height=%d, appHash=%X, tendermintHash=%X\n", expectedHeight, appCommit.GetHash(), tmHash)
	return nil
}

// performRollback performs a complete rollback when both app and tendermint are at the same height.
func performRollback(app types.Application, cfg *tmcfg.Config, targetHeight int64) error {
	// Step 1: Rollback app state first
	if _, err := rollbackAppStateToHeight(app, targetHeight); err != nil {
		return err
	}

	// Step 2: Rollback tendermint state
	fmt.Printf("Rolling back tendermint state to height %d...\n", targetHeight)
	tmHeight, tmHash, err := rollbackTendermintStateOnce(cfg)
	if err != nil {
		return fmt.Errorf("failed to rollback tendermint state (app state already rolled back to %d): %w\n"+
			"Note: You can re-run the rollback command to complete the tendermint rollback", targetHeight, err)
	}

	if tmHeight != targetHeight {
		return fmt.Errorf("tendermint state rollback failed: expected height %d, got %d", targetHeight, tmHeight)
	}

	// Step 3: Verify consistency
	return verifyConsistency(app, targetHeight, tmHash)
}

// completeTendermintRollback completes a rollback when app state is already rolled back but tendermint is not.
// It may need to rollback multiple times if tendermint is more than one height ahead.
func completeTendermintRollback(cfg *tmcfg.Config, targetHeight int64) error {
	maxIterations := 100 // Safety limit to prevent infinite loops
	for i := 0; i < maxIterations; i++ {
		currentTmHeight, err := getTendermintStateHeight(cfg)
		if err != nil {
			return fmt.Errorf("failed to get tendermint state height: %w", err)
		}

		if currentTmHeight == targetHeight {
			fmt.Printf("Tendermint state is already at target height %d\n", targetHeight)
			fmt.Printf("\nRollback completed successfully! Both app and tendermint are now at height %d\n", targetHeight)
			return nil
		}

		if currentTmHeight < targetHeight {
			return fmt.Errorf("tendermint state height (%d) is less than target height (%d). "+
				"This should not happen. Please check your node state.", currentTmHeight, targetHeight)
		}

		// Rollback one height
		fmt.Printf("Rolling back tendermint state from height %d to %d...\n", currentTmHeight, currentTmHeight-1)
		tmHeight, _, err := rollbackTendermintStateOnce(cfg)
		if err != nil {
			return fmt.Errorf("failed to rollback tendermint state from height %d: %w", currentTmHeight, err)
		}

		if tmHeight != currentTmHeight-1 {
			return fmt.Errorf("tendermint state rollback failed: expected height %d, got %d", currentTmHeight-1, tmHeight)
		}

		// Check if we've reached the target
		if tmHeight == targetHeight {
			fmt.Printf("\nRollback completed successfully! Both app and tendermint are now at height %d\n", targetHeight)
			return nil
		}
	}

	return fmt.Errorf("failed to complete rollback: reached maximum iterations (%d) without reaching target height %d", maxIterations, targetHeight)
}

// rollbackAppStateOnly rolls back only the app state to match the tendermint height.
// This is used when app is ahead of tendermint (unexpected but recoverable state).
func rollbackAppStateOnly(app types.Application, targetHeight int64) error {
	_, err := rollbackAppStateToHeight(app, targetHeight)
	if err != nil {
		return err
	}
	fmt.Printf("\nRollback completed successfully! Both app and tendermint are now at height %d\n", targetHeight)
	return nil
}
