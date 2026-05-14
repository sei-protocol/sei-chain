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
	// Validate prev block info — audit row 6 (LastBlockID, swallow-eligible).
	if !block.LastBlockID.Equals(state.LastBlockID) {
		if err := types.SwallowOrErr(policy, types.ErrorKindLastBlockID, logger,
			"internal/state/validation.go:LastBlockID", block.Height,
			state.LastBlockID, block.LastBlockID,
			"wrong Block.Header.LastBlockID.  Expected %v, got %v",
			state.LastBlockID, block.LastBlockID); err != nil {
			return err
		}
	}

	// Validate app info — audit row 1 (AppHash, swallow-eligible).
	if !bytes.Equal(block.AppHash, state.AppHash) {
		if err := types.SwallowOrErr(policy, types.ErrorKindAppHash, logger,
			"internal/state/validation.go:AppHash", block.Height,
			state.AppHash, block.AppHash,
			"wrong Block.Header.AppHash.  Expected %X, got %v",
			state.AppHash, block.AppHash); err != nil {
			return err
		}
	}
	// Audit row 7 (ConsensusHash, swallow-eligible).
	hashCP := state.ConsensusParams.HashConsensusParams()
	if !bytes.Equal(block.ConsensusHash, hashCP) {
		if err := types.SwallowOrErr(policy, types.ErrorKindConsensusHash, logger,
			"internal/state/validation.go:ConsensusHash", block.Height,
			hashCP, block.ConsensusHash,
			"wrong Block.Header.ConsensusHash.  Expected %X, got %v",
			hashCP, block.ConsensusHash); err != nil {
			return err
		}
	}
	// Audit row 8 (LastResultsHash, swallow-eligible).
	//
	// Giga production escape hatch — the pre-existing
	// tmtypes.SkipLastResultsHashValidation atomic.Bool is set
	// unconditionally by Giga at app init (app.go:749) and is load-bearing
	// for Giga's production halt-resistance on LastResultsHash. Migrating
	// Giga onto a build-tagged ConsensusPolicy variant is its own future
	// workstream. Until then this is the only Skip*-style early-return
	// guard preserved in the codebase; every other swallow-eligible site
	// computes its comparison and consults SwallowOrErr.
	if !types.SkipLastResultsHashValidation.Load() {
		if !bytes.Equal(block.LastResultsHash, state.LastResultsHash) {
			if err := types.SwallowOrErr(policy, types.ErrorKindLastResultsHash, logger,
				"internal/state/validation.go:LastResultsHash", block.Height,
				state.LastResultsHash, block.LastResultsHash,
				"wrong Block.Header.LastResultsHash.  Expected %X, got %v",
				state.LastResultsHash, block.LastResultsHash); err != nil {
				return err
			}
		}
	}
	// Audit row 9 (ValidatorsHash, swallow-eligible).
	if !bytes.Equal(block.ValidatorsHash, state.Validators.Hash()) {
		if err := types.SwallowOrErr(policy, types.ErrorKindValidatorsHash, logger,
			"internal/state/validation.go:ValidatorsHash", block.Height,
			state.Validators.Hash(), block.ValidatorsHash,
			"wrong Block.Header.ValidatorsHash.  Expected %X, got %v",
			state.Validators.Hash(), block.ValidatorsHash); err != nil {
			return err
		}
	}
	// Audit row 10 (NextValidatorsHash, swallow-eligible).
	if !bytes.Equal(block.NextValidatorsHash, state.NextValidators.Hash()) {
		if err := types.SwallowOrErr(policy, types.ErrorKindNextValidatorsHash, logger,
			"internal/state/validation.go:NextValidatorsHash", block.Height,
			state.NextValidators.Hash(), block.NextValidatorsHash,
			"wrong Block.Header.NextValidatorsHash.  Expected %X, got %v",
			state.NextValidators.Hash(), block.NextValidatorsHash); err != nil {
			return err
		}
	}

	// Validate block LastCommit.
	if block.Height == state.InitialHeight {
		if len(block.LastCommit.Signatures) != 0 {
			return errors.New("initial block can't have LastCommit signatures")
		}
	} else {
		// Audit row 12 (LastCommit signature verification, swallow-eligible).
		// LastCommit.Signatures length is checked in VerifyCommit.
		if err := state.LastValidators.VerifyCommit(
			state.ChainID, state.LastBlockID, block.Height-1, block.LastCommit); err != nil {
			if swErr := types.SwallowOrErr(policy, types.ErrorKindLastCommitVerify, logger,
				"internal/state/validation.go:LastCommitVerify", block.Height,
				state.LastBlockID, err.Error(),
				"VerifyCommit(): %w", err); swErr != nil {
				return swErr
			}
		}
	}

	// NOTE: We can't actually verify it's the right proposer because we don't
	// know what round the block was first proposed. So just check that it's
	// a legit address and a known validator.
	// The length is checked in ValidateBasic above.
	//
	// Audit row 13 (ProposerNotInValidatorSet, swallow-eligible).
	if !state.Validators.HasAddress(block.ProposerAddress) {
		if err := types.SwallowOrErr(policy, types.ErrorKindProposerNotInValidatorSet, logger,
			"internal/state/validation.go:ProposerNotInValidatorSet", block.Height,
			"<proposer in state.Validators>", block.ProposerAddress,
			"block.Header.ProposerAddress %X is not a validator",
			block.ProposerAddress); err != nil {
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

	// Check evidence doesn't exceed the limit amount of bytes — audit
	// row 17 (EvidenceOverflow, swallow-eligible).
	if max, got := state.ConsensusParams.Evidence.MaxBytes, block.Evidence.ByteSize(); got > max {
		if err := types.SwallowOrErr(policy, types.ErrorKindEvidenceOverflow, logger,
			"internal/state/validation.go:EvidenceOverflow", block.Height,
			max, got,
			"%w", types.NewErrEvidenceOverflow(max, got)); err != nil {
			return err
		}
	}

	return nil
}
