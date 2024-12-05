package server

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"
	tmcmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
)

var removeBlock = false

// NewRollbackCmd creates a command to rollback tendermint and multistore state by one height.
func NewRollbackCmd(appCreator types.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "rollback cosmos-sdk and tendermint state by one height",
		Long: `
A state rollback is performed to recover from an incorrect application state transition,
when Tendermint has persisted an incorrect app hash and is thus unable to make
progress. Rollback overwrites a state at height n with the state at height n - 1.
The application also roll back to height n - 1. No blocks are removed, so upon
restarting Tendermint the transactions in block n will be re-executed against the
application. If you wanna rollback multiple blocks, please add --hard option.
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

			// App State
			lastCommit := app.CommitMultiStore().LastCommitID()
			fmt.Printf("Initial App state height=%d and hash=%X\n", lastCommit.GetVersion(), lastCommit.GetHash())

			// rollback tendermint state
			tmHeight, hash, err := tmcmd.RollbackState(ctx.Config, removeBlock)
			if err != nil {
				return fmt.Errorf("failed to rollback tendermint state: %w", err)
			}
			fmt.Printf("Rolled back tendermint state to height %d and hash %X\n\n", tmHeight, hash)

			// rollback the app state
			lastCommit = app.CommitMultiStore().LastCommitID()
			fmt.Printf("CMS app state height %d and hash %X\n", lastCommit.GetVersion(), lastCommit.GetHash())
			fmt.Printf("Attempting to rollback app state to height=%d\n", tmHeight)
			if err := app.CommitMultiStore().RollbackToVersion(tmHeight); err != nil {
				return fmt.Errorf("failed to rollback to version: %w", err)
			}

			lastCommit = app.CommitMultiStore().LastCommitID()
			fmt.Printf("Rolled back app state to height %d and hash %X\n", lastCommit.GetVersion(), lastCommit.GetHash())

			// This will cause issues when you try starting the chain. Something probably went wrong
			if removeBlock && app.CommitMultiStore().LastCommitID().Version != tmHeight {
				panic("Application state height does not match the tendermint state height")
			}

			if removeBlock && string(lastCommit.GetHash()) != string(hash) {
				panic("Application state hash does not match the tendermint state hash")
			}

			// Need to delete entires in the WAL log to
			return nil
		},
	}

	cmd.Flags().String(flags.FlagChainID, "sei-chain", "genesis file chain-id, if left blank will use sei")
	cmd.Flags().BoolVar(&removeBlock, "hard", false, "remove last block as well as state")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}
