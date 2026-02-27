package server

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	tmcmd "github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/spf13/cobra"
)

// rollbackTendermintState rolls back the tendermint state by one height.
// Returns the new height and app hash.
func rollbackTendermintState(cfg *tmcfg.Config, targetHeight int64) (int64, []byte, error) {
	tmHeight, hash, err := tmcmd.RollbackStateToTargetHeight(cfg, true, targetHeight)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to rollback tendermint state: %w", err)
	}
	return tmHeight, hash, nil
}

// rollbackAppState rolls back the app state to the target height.
// Returns the final app hash.
func rollbackAppState(app types.Application, targetHeight int64) ([]byte, error) {
	lastCommit := app.CommitMultiStore().LastCommitID()
	fmt.Printf("CMS app state height %d and hash %X\n", lastCommit.GetVersion(), lastCommit.GetHash())
	fmt.Printf("Attempting to rollback app state to height=%d\n", targetHeight)

	if err := app.CommitMultiStore().RollbackToVersion(targetHeight); err != nil {
		return nil, fmt.Errorf("failed to rollback to version: %w", err)
	}

	lastCommit = app.CommitMultiStore().LastCommitID()
	fmt.Printf("Rolled back app state to height %d and hash %X\n", lastCommit.GetVersion(), lastCommit.GetHash())

	return lastCommit.GetHash(), nil
}

// NewRollbackCmd creates a command to rollback tendermint and multistore state by one height.
func NewRollbackCmd(appCreator types.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "rollback cosmos-sdk and tendermint state by one height",
		Long: `
A state rollback is performed to recover from an incorrect application state transition,
when Tendermint has persisted an incorrect app hash and is thus unable to make
progress. Rollback overwrites a state at height n with the state at height n - 1.
The application also rolls back to height n - 1. The last block is removed, so upon
restarting Tendermint the node will re-fetch and re-execute the transactions in block n.
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

			// Get initial app state
			lastCommit := app.CommitMultiStore().LastCommitID()
			appHeight := lastCommit.GetVersion()
			fmt.Printf("Initial App state height=%d and hash=%X\n", appHeight, lastCommit.GetHash())

			// Get tendermint state height
			tmState, err := tmcmd.LoadTendermintState(ctx.Config)
			tmHeight := tmState.LastBlockHeight
			if err != nil {
				return err
			}
			fmt.Printf("Initial Tendermint state height=%d\n", tmHeight)

			// Handle different scenarios based on height comparison
			if appHeight == tmHeight {
				// Scenario 1: Both at same height - normal rollback
				numBlocks, _ := cmd.Flags().GetInt64("num-blocks")
				targetHeight := appHeight - numBlocks
				if targetHeight < 0 {
					return fmt.Errorf("cannot rollback: current height is %d, cannot rollback below 0", appHeight)
				}
				fmt.Printf("Both app and tendermint are at height %d, performing normal rollback to target height %d\n", appHeight, targetHeight)

				// Rollback app state first
				appHash, err := rollbackAppState(app, targetHeight)
				if err != nil {
					return err
				}

				// Rollback tendermint state
				newTmHeight, stateHash, err := rollbackTendermintState(ctx.Config, targetHeight)
				if err != nil {
					return err
				}

				// Verify state height
				if app.CommitMultiStore().LastCommitID().Version != newTmHeight {
					panic("Application state height does not match the tendermint state height")
				}
				fmt.Printf("Rollback complete target height %d. App hash %X, state hash %X\n", newTmHeight, appHash, stateHash)

			} else if appHeight < tmHeight {
				// Scenario 2: App is behind tendermint - rollback tendermint only
				fmt.Printf("App is at height %d, tendermint is at height %d. Rolling back tendermint to target height %d\n", appHeight, tmHeight, appHeight)

				newTmHeight, stateHash, err := rollbackTendermintState(ctx.Config, appHeight)
				if err != nil {
					return err
				}

				// Check if heights now match
				if appHeight != newTmHeight {
					fmt.Printf("WARNING: After rollback, app height (%d) still doesn't match tendermint height (%d). You may need to run rollback again.\n",
						appHeight, newTmHeight)
				} else {
					fmt.Printf("Rollback complete to target height %d. App hash %X, state hash %X\n", newTmHeight, lastCommit.Hash, stateHash)
				}
			} else {
				// Scenario 3: App is ahead of tendermint - rollback app only
				fmt.Printf("App is at height %d, tendermint is at height %d. Rolling back app to target height %d\n", appHeight, tmHeight, tmHeight)

				appHash, err := rollbackAppState(app, tmHeight)
				if err != nil {
					return err
				}

				// Verify app is now at tendermint height
				if app.CommitMultiStore().LastCommitID().Version != tmHeight {
					return fmt.Errorf("failed to rollback app to tendermint height: expected %d, got %d",
						tmHeight, app.CommitMultiStore().LastCommitID().Version)
				}

				fmt.Printf("Rollback complete to target height %d. App hash %X, state hash %X\n", tmHeight, appHash, tmState.AppHash)
			}

			return nil
		},
	}

	cmd.Flags().Int64P("num-blocks", "n", 1, "number of blocks to rollback")
	cmd.Flags().String(flags.FlagChainID, "sei-chain", "genesis file chain-id, if left blank will use sei")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}
