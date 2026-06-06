package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

//-----------------------------------------------------
// Validate block

// Swallow-eligible failure sites in this function consult
// types.ConsensusPolicy via HandleError(kind, err); the audit-row
// ErrorKind passed at each site is the cross-reference (see
// types.ErrorKind*).
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
		if err := policy.HandleError(types.ErrorKindLastBlockID,
			fmt.Errorf("wrong Block.Header.LastBlockID.  Expected %v, got %v",
				state.LastBlockID, block.LastBlockID)); err != nil {
			return err
		}
	}

	// Validate app info.
	if !bytes.Equal(block.AppHash, state.AppHash) {
		if err := policy.HandleError(types.ErrorKindAppHash,
			fmt.Errorf("wrong Block.Header.AppHash.  Expected %X, got %v",
				state.AppHash, block.AppHash)); err != nil {
			return err
		}
	}
	hashCP := state.ConsensusParams.HashConsensusParams()
	if !bytes.Equal(block.ConsensusHash, hashCP) {
		if err := policy.HandleError(types.ErrorKindConsensusHash,
			fmt.Errorf("wrong Block.Header.ConsensusHash.  Expected %X, got %v",
				hashCP, block.ConsensusHash)); err != nil {
			return err
		}
	}
	// Giga production escape hatch: tmtypes.SkipLastResultsHashValidation
	// is set unconditionally by Giga at app init (app.go:749) and is
	// load-bearing for Giga's production halt-resistance on
	// LastResultsHash. This is the only Skip*-style early-return preserved
	// in the codebase; migrating Giga onto a build-tagged ConsensusPolicy
	// variant is its own future workstream.
	if !types.SkipLastResultsHashValidation.Load() {
		if !bytes.Equal(block.LastResultsHash, state.LastResultsHash) {
			if err := policy.HandleError(types.ErrorKindLastResultsHash,
				fmt.Errorf("wrong Block.Header.LastResultsHash.  Expected %X, got %v",
					state.LastResultsHash, block.LastResultsHash)); err != nil {
				return err
			}
		}
	}
	if !bytes.Equal(block.ValidatorsHash, state.Validators.Hash()) {
		if err := policy.HandleError(types.ErrorKindValidatorsHash,
			fmt.Errorf("wrong Block.Header.ValidatorsHash.  Expected %X, got %v",
				state.Validators.Hash(), block.ValidatorsHash)); err != nil {
			return err
		}
	}
	if !bytes.Equal(block.NextValidatorsHash, state.NextValidators.Hash()) {
		if err := policy.HandleError(types.ErrorKindNextValidatorsHash,
			fmt.Errorf("wrong Block.Header.NextValidatorsHash.  Expected %X, got %v",
				state.NextValidators.Hash(), block.NextValidatorsHash)); err != nil {
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
			if swErr := policy.HandleError(types.ErrorKindLastCommitVerify,
				fmt.Errorf("VerifyCommit(): %w", err)); swErr != nil {
				return swErr
			}
		}
	}

	// NOTE: We can't actually verify it's the right proposer because we don't
	// know what round the block was first proposed. So just check that it's
	// a legit address and a known validator.
	// The length is checked in ValidateBasic above.
	if !state.Validators.HasAddress(block.ProposerAddress) {
		if err := policy.HandleError(types.ErrorKindProposerNotInValidatorSet,
			fmt.Errorf("block.Header.ProposerAddress %X is not a validator",
				block.ProposerAddress)); err != nil {
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
		if err := policy.HandleError(types.ErrorKindEvidenceOverflow,
			types.NewErrEvidenceOverflow(max, got)); err != nil {
			return err
		}
	}

	return nil
}
