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
		data.VoteDelegationBackfillCutoff == other.VoteDelegationBackfillCutoff &&
		modernTallyRoundProposalIDsEqual(data.ModernTallyRoundProposalIds, other.ModernTallyRoundProposalIds) &&
		data.Deposits.Equal(other.Deposits) &&
		data.Votes.Equal(other.Votes) &&
		data.Proposals.Equal(other.Proposals) &&
		data.DepositParams.Equal(other.DepositParams) &&
		data.TallyParams.Equal(other.TallyParams) &&
		data.VotingParams.Equal(other.VotingParams) &&
		voteDelegationSnapshotsEqual(data.VoteDelegationSnapshots, other.VoteDelegationSnapshots) &&
		tallyElectoratesEqual(data.TallyElectorates, other.TallyElectorates)
}

func modernTallyRoundProposalIDsEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

func tallyElectoratesEqual(a, b []TallyElectorate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ProposalId != b[i].ProposalId ||
			!a[i].TotalBondedTokens.Equal(b[i].TotalBondedTokens) ||
			!a[i].TallyParams.Equal(b[i].TallyParams) ||
			len(a[i].TallyValidators) != len(b[i].TallyValidators) {
			return false
		}
		for j := range a[i].TallyValidators {
			if a[i].TallyValidators[j].Address != b[i].TallyValidators[j].Address ||
				!a[i].TallyValidators[j].BondedTokens.Equal(b[i].TallyValidators[j].BondedTokens) ||
				!a[i].TallyValidators[j].DelegatorShares.Equal(b[i].TallyValidators[j].DelegatorShares) {
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
	if err := validateTallyElectorates(data.Proposals, data.TallyElectorates); err != nil {
		return err
	}
	if err := validateTallyElectorateVoteSnapshots(data.Votes, data.VoteDelegationSnapshots, data.TallyElectorates); err != nil {
		return err
	}
	if err := validateModernTallyRounds(data.Proposals, data.VoteDelegationBackfillCutoff, data.ModernTallyRoundProposalIds); err != nil {
		return err
	}
	if err := validateModernTallyRoundVoteSnapshots(data.Votes, data.VoteDelegationSnapshots, data.ModernTallyRoundProposalIds); err != nil {
		return err
	}

	return nil
}

func validateModernTallyRounds(proposals Proposals, cutoff uint64, proposalIDs []uint64) error {
	proposalsByID := make(map[uint64]Proposal, len(proposals))
	for _, proposal := range proposals {
		proposalsByID[proposal.ProposalId] = proposal
	}

	seen := make(map[uint64]struct{}, len(proposalIDs))
	for _, proposalID := range proposalIDs {
		proposal, found := proposalsByID[proposalID]
		if !found || proposal.Status != StatusVotingPeriod {
			return fmt.Errorf("modern tally round for proposal %d has no voting-period proposal", proposalID)
		}
		if cutoff == 0 || proposalID >= cutoff {
			return fmt.Errorf("modern tally round for proposal %d is not legacy", proposalID)
		}
		if proposal.IsExpedited {
			return fmt.Errorf("modern tally round for proposal %d is expedited", proposalID)
		}
		if _, found := seen[proposalID]; found {
			return fmt.Errorf("duplicate modern tally round for proposal %d", proposalID)
		}
		seen[proposalID] = struct{}{}
	}
	return nil
}

func validateModernTallyRoundVoteSnapshots(
	votes Votes,
	snapshots []VoteDelegationSnapshot,
	proposalIDs []uint64,
) error {
	modernRounds := make(map[uint64]struct{}, len(proposalIDs))
	for _, proposalID := range proposalIDs {
		modernRounds[proposalID] = struct{}{}
	}
	snapshotKeys := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotKeys[fmt.Sprintf("%d/%s", snapshot.ProposalId, snapshot.Voter)] = struct{}{}
	}
	for _, vote := range votes {
		if _, found := modernRounds[vote.ProposalId]; !found {
			continue
		}
		key := fmt.Sprintf("%d/%s", vote.ProposalId, vote.Voter)
		if _, found := snapshotKeys[key]; !found {
			return fmt.Errorf("modern tally round vote %s has no delegation snapshot", key)
		}
	}
	return nil
}

func validateTallyElectorateVoteSnapshots(
	votes Votes,
	snapshots []VoteDelegationSnapshot,
	electorates []TallyElectorate,
) error {
	electorateProposals := make(map[uint64]struct{}, len(electorates))
	for _, electorate := range electorates {
		electorateProposals[electorate.ProposalId] = struct{}{}
	}
	snapshotKeys := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotKeys[fmt.Sprintf("%d/%s", snapshot.ProposalId, snapshot.Voter)] = struct{}{}
	}
	for _, vote := range votes {
		if _, found := electorateProposals[vote.ProposalId]; !found {
			continue
		}
		key := fmt.Sprintf("%d/%s", vote.ProposalId, vote.Voter)
		if _, found := snapshotKeys[key]; !found {
			return fmt.Errorf("tally electorate vote %s has no delegation snapshot", key)
		}
	}
	return nil
}

func validateTallyElectorates(proposals Proposals, electorates []TallyElectorate) error {
	votingProposals := make(map[uint64]struct{}, len(proposals))
	for _, proposal := range proposals {
		if proposal.Status == StatusVotingPeriod {
			votingProposals[proposal.ProposalId] = struct{}{}
		}
	}

	seenElectorates := make(map[uint64]struct{}, len(electorates))
	for _, electorate := range electorates {
		if _, found := votingProposals[electorate.ProposalId]; !found {
			return fmt.Errorf("tally electorate for proposal %d has no voting-period proposal", electorate.ProposalId)
		}
		if _, found := seenElectorates[electorate.ProposalId]; found {
			return fmt.Errorf("duplicate tally electorate for proposal %d", electorate.ProposalId)
		}
		seenElectorates[electorate.ProposalId] = struct{}{}
		if electorate.TotalBondedTokens.IsNil() {
			return fmt.Errorf("tally electorate total bonded tokens are not initialized")
		}
		if electorate.TotalBondedTokens.IsNegative() {
			return fmt.Errorf("tally electorate total bonded tokens cannot be negative: %s", electorate.TotalBondedTokens)
		}
		if err := validateTallyParams(electorate.TallyParams); err != nil {
			return fmt.Errorf("invalid tally electorate params for proposal %d: %w", electorate.ProposalId, err)
		}

		seenValidators := make(map[string]struct{}, len(electorate.TallyValidators))
		validatorTokens := sdk.ZeroInt()
		for _, validator := range electorate.TallyValidators {
			if _, err := sdk.ValAddressFromBech32(validator.Address); err != nil {
				return fmt.Errorf("invalid tally electorate validator %q: %w", validator.Address, err)
			}
			if _, found := seenValidators[validator.Address]; found {
				return fmt.Errorf("duplicate tally electorate validator %q for proposal %d", validator.Address, electorate.ProposalId)
			}
			seenValidators[validator.Address] = struct{}{}
			if validator.BondedTokens.IsNil() {
				return fmt.Errorf("tally electorate validator bonded tokens are not initialized")
			}
			if !validator.BondedTokens.IsPositive() {
				return fmt.Errorf("tally electorate validator bonded tokens must be positive: %s", validator.BondedTokens)
			}
			if validator.DelegatorShares.IsNil() {
				return fmt.Errorf("tally electorate validator shares are not initialized")
			}
			if !validator.DelegatorShares.IsPositive() {
				return fmt.Errorf("tally electorate validator shares must be positive: %s", validator.DelegatorShares)
			}
			validatorTokens = validatorTokens.Add(validator.BondedTokens)
		}
		if !validatorTokens.Equal(electorate.TotalBondedTokens) {
			return fmt.Errorf(
				"tally electorate validator tokens %s do not equal total bonded tokens %s",
				validatorTokens,
				electorate.TotalBondedTokens,
			)
		}
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
