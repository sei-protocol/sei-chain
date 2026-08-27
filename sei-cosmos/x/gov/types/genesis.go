package types

import (
	"fmt"

	codecTypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// NewGenesisState creates a new genesis state for the governance module
func NewGenesisState(startingProposalID uint64, dp DepositParams, vp VotingParams, tp TallyParams) *GenesisState {
	return &GenesisState{
		StartingProposalId: startingProposalID,
		DepositParams:      dp,
		VotingParams:       vp,
		TallyParams:        tp,
	}
}

// DefaultGenesisState defines the default governance genesis state
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultStartingProposalID,
		DefaultDepositParams(),
		DefaultVotingParams(),
		DefaultTallyParams(),
	)
}

func (data GenesisState) Equal(other GenesisState) bool {
	return data.StartingProposalId == other.StartingProposalId &&
		data.Deposits.Equal(other.Deposits) &&
		data.Votes.Equal(other.Votes) &&
		data.Proposals.Equal(other.Proposals) &&
		data.DepositParams.Equal(other.DepositParams) &&
		data.TallyParams.Equal(other.TallyParams) &&
		data.VotingParams.Equal(other.VotingParams) &&
		voteDelegationSnapshotsEqual(data.VoteDelegationSnapshots, other.VoteDelegationSnapshots)
}

func voteDelegationSnapshotsEqual(a, b []VoteDelegationSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ProposalId != b[i].ProposalId || a[i].Voter != b[i].Voter || len(a[i].Delegations) != len(b[i].Delegations) {
			return false
		}
		for j := range a[i].Delegations {
			if a[i].Delegations[j].Validator != b[i].Delegations[j].Validator ||
				!a[i].Delegations[j].Shares.Equal(b[i].Delegations[j].Shares) {
				return false
			}
		}
	}
	return true
}

// Empty returns true if a GenesisState is empty
func (data GenesisState) Empty() bool {
	return data.Equal(GenesisState{})
}

// ValidateGenesis checks if parameters are within valid ranges
func ValidateGenesis(data *GenesisState) error {
	if data == nil {
		return fmt.Errorf("governance genesis state cannot be nil")
	}

	if data.Empty() {
		return fmt.Errorf("governance genesis state cannot be nil")
	}

	if err := validateTallyParams(data.TallyParams); err != nil {
		return err
	}

	if !data.DepositParams.MinDeposit.IsValid() {
		return fmt.Errorf("governance deposit amount must be a valid sdk.Coins amount, is %s",
			data.DepositParams.MinDeposit.String())
	}

	if !data.DepositParams.MinExpeditedDeposit.IsValid() {
		return fmt.Errorf("governance min expedited deposit amount must be a valid sdk.Coins amount, is %s",
			data.DepositParams.MinExpeditedDeposit.String())
	}

	if data.DepositParams.MinExpeditedDeposit.IsAllLTE(data.DepositParams.MinDeposit) {
		return fmt.Errorf("governance min expedited deposit amount %s must be greater than regular min deposit %s",
			data.DepositParams.MinExpeditedDeposit.String(),
			data.DepositParams.MinDeposit.String())
	}

	if err := validateVoteDelegationSnapshots(data.Votes, data.VoteDelegationSnapshots); err != nil {
		return err
	}

	return nil
}

func validateVoteDelegationSnapshots(votes Votes, snapshots []VoteDelegationSnapshot) error {
	voteKeys := make(map[string]struct{}, len(votes))
	for _, vote := range votes {
		voteKeys[fmt.Sprintf("%d/%s", vote.ProposalId, vote.Voter)] = struct{}{}
	}

	seenSnapshots := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		key := fmt.Sprintf("%d/%s", snapshot.ProposalId, snapshot.Voter)
		if _, found := voteKeys[key]; !found {
			return fmt.Errorf("vote delegation snapshot %s has no matching vote", key)
		}
		if _, found := seenSnapshots[key]; found {
			return fmt.Errorf("duplicate vote delegation snapshot %s", key)
		}
		seenSnapshots[key] = struct{}{}

		if _, err := sdk.AccAddressFromBech32(snapshot.Voter); err != nil {
			return fmt.Errorf("invalid vote delegation snapshot voter %q: %w", snapshot.Voter, err)
		}
		seenValidators := make(map[string]struct{}, len(snapshot.Delegations))
		for _, delegation := range snapshot.Delegations {
			if _, err := sdk.ValAddressFromBech32(delegation.Validator); err != nil {
				return fmt.Errorf("invalid vote delegation snapshot validator %q: %w", delegation.Validator, err)
			}
			if !delegation.Shares.IsPositive() {
				return fmt.Errorf("vote delegation snapshot shares must be positive: %s", delegation.Shares)
			}
			if _, found := seenValidators[delegation.Validator]; found {
				return fmt.Errorf("duplicate validator %q in vote delegation snapshot %s", delegation.Validator, key)
			}
			seenValidators[delegation.Validator] = struct{}{}
		}
	}
	return nil
}

var _ codecTypes.UnpackInterfacesMessage = GenesisState{}

// UnpackInterfaces implements UnpackInterfacesMessage.UnpackInterfaces
func (data GenesisState) UnpackInterfaces(unpacker codecTypes.AnyUnpacker) error {
	for _, p := range data.Proposals {
		err := p.UnpackInterfaces(unpacker)
		if err != nil {
			return err
		}
	}
	return nil
}
