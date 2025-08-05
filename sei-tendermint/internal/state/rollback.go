package state

import (
	"errors"
	"fmt"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/version"
)

func resetPrivValidatorConfig(privValidatorConfig config.PrivValidatorConfig) error {
	// Priv Val LastState needs to be rolled back if this is the case
	filePv, loadErr := privval.LoadFilePV(privValidatorConfig.KeyFile(), privValidatorConfig.StateFile())
	if loadErr != nil {
		return fmt.Errorf("failed to load private validator file: %w", loadErr)
	}

	resetErr := filePv.Reset()
	if resetErr != nil {
		return fmt.Errorf("failed to reset private validator file: %w", resetErr)
	}

	return nil
}

// Rollback overwrites the current Tendermint state (height n) with the most
// recent previous state (height n - 1).
// Note that this function does not affect application state.
func Rollback(bs BlockStore, ss Store, removeBlock bool, privValidatorConfig *config.PrivValidatorConfig) (int64, []byte, error) {
	latestState, err := ss.Load()
	if err != nil {
		return -1, nil, err
	}
	if latestState.IsEmpty() {
		return -1, nil, errors.New("no state found")
	}

	latestBlockHeight := bs.Height()
	latestStateHeight := latestState.LastBlockHeight
	fmt.Printf("Current blockStore height=%d tendermint state height=%d appHash=%X lastResultHash=%X\n", latestBlockHeight, latestStateHeight, latestState.AppHash, latestState.LastResultsHash)

	// NOTE: persistence of state and blocks don't happen atomically. Therefore it is possible that
	// when the user stopped the node the state wasn't updated but the blockstore was. Discard the
	// pending block before continuing.
	if latestBlockHeight == latestStateHeight+1 {
		fmt.Printf("Invalid state in the latest block height=%d, removing it first \n", latestBlockHeight)
		if err := bs.DeleteLatestBlock(); err != nil {
			return -1, nil, fmt.Errorf("failed to remove final block from blockstore: %w", err)
		}
		return latestState.LastBlockHeight, latestState.AppHash, nil
	}

	// If the state store isn't one below nor equal to the blockstore height than this violates the
	// invariant
	if latestBlockHeight != latestState.LastBlockHeight {
		return -1, nil, fmt.Errorf("statestore height (%d) is not one below or equal to blockstore height (%d)",
			latestState.LastBlockHeight, latestBlockHeight)
	}

	// state store height is equal to blockstore height. We're good to proceed with rolling back state
	rollbackHeight := latestState.LastBlockHeight - 1
	rollbackBlock := bs.LoadBlockMeta(rollbackHeight)
	if rollbackBlock == nil {
		return -1, nil, fmt.Errorf("block at height %d not found", rollbackHeight)
	}

	// we also need to retrieve the latest block because the app hash and last results hash is only agreed upon in the following block
	latestBlock := bs.LoadBlockMeta(latestState.LastBlockHeight)
	if latestBlock == nil {
		return -1, nil, fmt.Errorf("block at height %d not found", latestState.LastBlockHeight)
	}

	previousParams, err := ss.LoadConsensusParams(rollbackHeight + 1)
	if err != nil {
		return -1, nil, err
	}

	valChangeHeight := latestState.LastHeightValidatorsChanged
	if valChangeHeight > rollbackHeight {
		valInfo, err := ss.(dbStore).LoadValidatorsInfo(rollbackHeight)
		if err != nil {
			return -1, nil, err
		}
		valChangeHeight = valInfo.LastHeightChanged
	}

	previousLastValidatorSet, err := ss.LoadValidators(rollbackHeight)
	if err != nil {
		return -1, nil, err
	}

	paramsChangeHeight := latestState.LastHeightConsensusParamsChanged
	// this can only happen if params changed from the last block
	if paramsChangeHeight > rollbackHeight {
		paramsChangeHeight = rollbackHeight + 1
	}

	rolledBackHeight := rollbackBlock.Header.Height
	rolledBackAppHash := latestBlock.Header.AppHash
	rolledBackLastResultHash := latestBlock.Header.LastResultsHash

	fmt.Printf("Rollback block Height=%d, appHash=%X\n", rollbackBlock.Header.Height, rollbackBlock.Header.AppHash)
	fmt.Printf("Latest block Height=%d, appHash=%X\n", latestBlock.Header.Height, latestBlock.Header.AppHash)

	// build the new state from the old state and the prior block
	rolledBackState := State{
		Version: Version{
			Consensus: version.Consensus{
				Block: version.BlockProtocol,
				App:   previousParams.Version.AppVersion,
			},
			Software: version.TMVersion,
		},
		// immutable fields
		ChainID:       latestState.ChainID,
		InitialHeight: latestState.InitialHeight,

		LastBlockHeight: rolledBackHeight,
		LastBlockID:     rollbackBlock.BlockID,
		LastBlockTime:   rollbackBlock.Header.Time,

		AppHash:         rolledBackAppHash,
		LastResultsHash: rolledBackLastResultHash,

		NextValidators:              latestState.Validators,
		Validators:                  latestState.LastValidators,
		LastValidators:              previousLastValidatorSet,
		LastHeightValidatorsChanged: valChangeHeight,

		ConsensusParams:                  previousParams,
		LastHeightConsensusParamsChanged: paramsChangeHeight,
	}

	// persist the new state. This overrides the invalid one. NOTE: this will also
	// persist the validator set and consensus params over the existing structures,
	// but both should be the same
	if err := ss.Save(rolledBackState); err != nil {
		return -1, nil, fmt.Errorf("failed to save rolled back state: %w", err)
	}

	// If removeBlock is true then also remove the block associated with the previous state.
	// This will mean both the last state and last block height is equal to n - 1
	if removeBlock {
		if err := bs.DeleteLatestBlock(); err != nil {
			return -1, nil, fmt.Errorf("failed to remove final block from blockstore: %w", err)
		}

		err = resetPrivValidatorConfig(*privValidatorConfig)
		if err != nil {
			return -1, nil, err
		}
	}

	fmt.Printf("Saved tendermint state height=%d, appHash=%X, lastResultHash=%X\n", rolledBackHeight, rolledBackState.AppHash, rolledBackState.LastResultsHash)
	return rolledBackHeight, rolledBackState.AppHash, nil
}
