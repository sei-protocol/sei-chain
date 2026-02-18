package commands

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
)

// LoadTendermintState loads the tendermint state from the database.
// Returns the state, or an error if loading fails.
func LoadTendermintState(config *config.Config) (state.State, error) {
	blockStore, stateStore, err := loadStateAndBlockStore(config)
	if err != nil {
		return state.State{}, err
	}
	defer func() {
		_ = blockStore.Close()
		_ = stateStore.Close()
	}()

	tmState, err := stateStore.Load()
	if err != nil || tmState.IsEmpty() {
		return state.State{}, fmt.Errorf("failed to load state: %w", err)
	}

	return tmState, nil
}

// RollbackStateToTargetHeight rolls back the tendermint state to the target height.
// It repeatedly calls state.Rollback until the target height is reached.
func RollbackStateToTargetHeight(config *config.Config, removeBlock bool, targetHeight int64) (int64, []byte, error) {
	// use the parsed config to load the block and state store
	blockStore, stateStore, err := loadStateAndBlockStore(config)
	if err != nil {
		return -1, nil, err
	}

	defer func() {
		_ = blockStore.Close()
		_ = stateStore.Close()
	}()

	// Get initial state to verify we are above target height
	tmState, err := stateStore.Load()
	if err != nil || tmState.IsEmpty() {
		return 0, nil, fmt.Errorf("failed to load state: %w", err)
	}
	tmStateHeight := tmState.LastBlockHeight
	blockStoreHeight := blockStore.Height()

	if tmStateHeight <= targetHeight && blockStoreHeight == tmStateHeight {
		return tmState.LastBlockHeight, tmState.AppHash, nil
	}

	var currentHeight int64
	var hash []byte

	for {
		// Since state.Rollback modifies the store, we can just call it in a loop.
		currentHeight, hash, err = state.Rollback(blockStore, stateStore, removeBlock, config.PrivValidator)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to rollback state: %w", err)
		}

		fmt.Printf("Rolled back tendermint state to height %d and hash %X\n", currentHeight, hash)

		if currentHeight <= targetHeight {
			break
		}
	}

	return currentHeight, hash, nil
}
