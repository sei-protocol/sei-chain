package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

//-----------------------------------------------------
// Validate block

func validateBlock(state State, block *types.Block, policy types.ConsensusPolicy) error {
	// Validate internal consistency.
	if err := block.ValidateBasic(policy); err != nil {
		return fmt.Errorf("ValidateBasic(): %w", err)
	}

	// Validate basic info.
	if block.Version != state.Version.Consensus {
		return fmt.Errorf("wrong Block.Header.Version. Expected %v, got %v",
			state.Version.Consensus,
			block.Version,
		)
	}
	if block.ChainID != state.ChainID {
		return fmt.Errorf("wrong Block.Header.ChainID. Expected %v, got %v",
			state.ChainID,
			block.ChainID,
		)
	}
	if state.LastBlockHeight == 0 && block.Height != state.InitialHeight {
		return fmt.Errorf("wrong Block.Header.Height. Expected %v for initial block, got %v",
			block.Height, state.InitialHeight)
	}
	if state.LastBlockHeight > 0 && block.Height != state.LastBlockHeight+1 {
		return fmt.Errorf("wrong Block.Header.Height. Expected %v, got %v",
			state.LastBlockHeight+1,
			block.Height,
		)
	}
	// Validate prev block info.
	if !block.LastBlockID.Equals(state.LastBlockID) {
		if err := policy.HandleError(fmt.Errorf(
			"wrong Block.Header.LastBlockID: expected %v, got %v: %w",
			state.LastBlockID, block.LastBlockID, types.ErrLastBlockID)); err != nil {
			return err
		}
	}

	// Validate app info.
	if !bytes.Equal(block.AppHash, state.AppHash) {
		if err := policy.HandleError(fmt.Errorf(
			"wrong Block.Header.AppHash: expected %X, got %X: %w",
			state.AppHash, block.AppHash, types.ErrAppHash)); err != nil {
			return err
		}
	}
	hashCP := state.ConsensusParams.HashConsensusParams()
	if !bytes.Equal(block.ConsensusHash, hashCP) {
		if err := policy.HandleError(fmt.Errorf(
			"wrong Block.Header.ConsensusHash: expected %X, got %X: %w",
			hashCP, block.ConsensusHash, types.ErrConsensusHash)); err != nil {
			return err
		}
	}
	// Giga escape hatch (set in app.go via tmtypes.SkipLastResultsHashValidation.Store) — only Skip-style guard preserved.
	if !types.SkipLastResultsHashValidation.Load() {
		if !bytes.Equal(block.LastResultsHash, state.LastResultsHash) {
			if err := policy.HandleError(fmt.Errorf(
				"wrong Block.Header.LastResultsHash: expected %X, got %X: %w",
				state.LastResultsHash, block.LastResultsHash, types.ErrLastResultsHash)); err != nil {
				return err
			}
		}
	}
	if !bytes.Equal(block.ValidatorsHash, state.Validators.Hash()) {
		if err := policy.HandleError(fmt.Errorf(
			"wrong Block.Header.ValidatorsHash: expected %X, got %X: %w",
			state.Validators.Hash(), block.ValidatorsHash, types.ErrValidatorsHash)); err != nil {
			return err
		}
	}
	if !bytes.Equal(block.NextValidatorsHash, state.NextValidators.Hash()) {
		if err := policy.HandleError(fmt.Errorf(
			"wrong Block.Header.NextValidatorsHash: expected %X, got %X: %w",
			state.NextValidators.Hash(), block.NextValidatorsHash, types.ErrNextValidatorsHash)); err != nil {
			return err
		}
	}

	// Validate block LastCommit.
	if block.Height == state.InitialHeight {
		if len(block.LastCommit.Signatures) != 0 {
			return errors.New("initial block can't have LastCommit signatures")
		}
	} else {
		// LastCommit.Signatures length is checked in VerifyCommit.
		if err := state.LastValidators.VerifyCommit(
			state.ChainID, state.LastBlockID, block.Height-1, block.LastCommit); err != nil {
			if swErr := policy.HandleError(fmt.Errorf(
				"%w: %w", types.ErrLastCommitVerify, err)); swErr != nil {
				return swErr
			}
		}
	}

	// NOTE: We can't actually verify it's the right proposer because we don't
	// know what round the block was first proposed. So just check that it's
	// a legit address and a known validator.
	// The length is checked in ValidateBasic above.
	if !state.Validators.HasAddress(block.ProposerAddress) {
		if err := policy.HandleError(fmt.Errorf(
			"block.Header.ProposerAddress %X is not a validator: %w",
			block.ProposerAddress, types.ErrProposerNotInValidatorSet)); err != nil {
			return err
		}
	}

	// Validate block Time
	switch {
	case block.Height > state.InitialHeight:
		if !block.Time.After(state.LastBlockTime) {
			return fmt.Errorf("block time %v not greater than last block time %v",
				block.Time,
				state.LastBlockTime,
			)
		}

	case block.Height == state.InitialHeight:
		genesisTime := state.LastBlockTime
		if block.Time.Before(genesisTime) {
			return fmt.Errorf("block time %v is before genesis time %v",
				block.Time,
				genesisTime,
			)
		}

	default:
		return fmt.Errorf("block height %v lower than initial height %v",
			block.Height, state.InitialHeight)
	}

	// Check evidence doesn't exceed the limit amount of bytes.
	if max, got := state.ConsensusParams.Evidence.MaxBytes, block.Evidence.ByteSize(); got > max {
		if err := policy.HandleError(fmt.Errorf(
			"%w: %w", types.ErrTooMuchEvidence, types.NewErrEvidenceOverflow(max, got))); err != nil {
			return err
		}
	}

	return nil
}
