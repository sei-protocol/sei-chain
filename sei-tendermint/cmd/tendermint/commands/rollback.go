package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/state"
)

var removeBlock bool = false

func MakeRollbackStateCommand(conf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "rollback tendermint state by one height",
		Long: `
A state rollback is performed to recover from an incorrect application state transition,
when Tendermint has persisted an incorrect app hash and is thus unable to make
progress. Rollback overwrites a state at height n with the state at height n - 1.
The application should also roll back to height n - 1. No blocks are removed, so upon
restarting Tendermint the transactions in block n will be re-executed against the
application.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			height, hash, err := RollbackState(conf, removeBlock)
			if err != nil {
				return fmt.Errorf("failed to rollback state: %w", err)
			}

			if removeBlock {
				fmt.Printf("Rolled back both state and block to height %d and hash %X\n", height, hash)
			} else {
				fmt.Printf("Rolled back state to height %d and hash %X\n", height, hash)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&removeBlock, "hard", false, "remove last block as well as state")

	return cmd
}

// RollbackState takes the state at the current height n and overwrites it with the state
// at height n - 1. Note state here refers to tendermint state not application state.
// Returns the latest state height and app hash alongside an error if there was one.
func RollbackState(config *config.Config, removeBlock bool) (int64, []byte, error) {
	// use the parsed config to load the block and state store
	blockStore, stateStore, err := loadStateAndBlockStore(config)
	if err != nil {
		return -1, nil, err
	}

	defer func() {
		_ = blockStore.Close()
		_ = stateStore.Close()
	}()

	// rollback the last state
	height, hash, err := state.Rollback(blockStore, stateStore, removeBlock, config.PrivValidator)
	return height, hash, err
}
