package types

import (
	"bytes"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestEqualProposalID(t *testing.T) {
	state1 := GenesisState{}
	state2 := GenesisState{}
	require.Equal(t, state1, state2)

	// Proposals
	state1.StartingProposalId = 1
	require.NotEqual(t, state1, state2)
	require.False(t, state1.Equal(state2))

	state2.StartingProposalId = 1
	require.Equal(t, state1, state2)
	require.True(t, state1.Equal(state2))
}

func TestGenesisStateEqualIncludesTallyElectorates(t *testing.T) {
	state1 := GenesisState{TallyElectorates: []TallyElectorate{validTallyElectorate(1)}}
	state2 := state1
	require.True(t, state1.Equal(state2))

	state2.TallyElectorates = nil
	require.False(t, state1.Equal(state2))
}

func TestGenesisStateEqualIncludesVoteDelegationBackfillCutoff(t *testing.T) {
	state1 := GenesisState{VoteDelegationBackfillCutoff: 3}
	state2 := state1
	require.True(t, state1.Equal(state2))

	state2.VoteDelegationBackfillCutoff = 4
	require.False(t, state1.Equal(state2))
}

func TestGenesisStateEqualIncludesModernTallyRounds(t *testing.T) {
	state1 := GenesisState{ModernTallyRoundProposalIds: []uint64{1}}
	state2 := state1
	require.True(t, state1.Equal(state2))

	state2.ModernTallyRoundProposalIds = nil
	require.False(t, state1.Equal(state2))
}

func TestValidateGenesisModernTallyRounds(t *testing.T) {
	state := DefaultGenesisState()
	state.VoteDelegationBackfillCutoff = 2
	state.Proposals = Proposals{{ProposalId: 1, Status: StatusVotingPeriod}}
	state.ModernTallyRoundProposalIds = []uint64{1}
	require.NoError(t, ValidateGenesis(state))

	state.ModernTallyRoundProposalIds = []uint64{1, 1}
	require.ErrorContains(t, ValidateGenesis(state), "duplicate modern tally round")

	state.ModernTallyRoundProposalIds = []uint64{2}
	require.ErrorContains(t, ValidateGenesis(state), "has no voting-period proposal")

	state.Proposals[0].IsExpedited = true
	state.ModernTallyRoundProposalIds = []uint64{1}
	require.ErrorContains(t, ValidateGenesis(state), "is expedited")

	state.Proposals[0].IsExpedited = false
	voter := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	state.Votes = Votes{NewVote(1, voter, NewNonSplitVoteOption(OptionYes))}
	require.ErrorContains(t, ValidateGenesis(state), "modern tally round vote")

	state.VoteDelegationSnapshots = []VoteDelegationSnapshot{{ProposalId: 1, Voter: voter.String()}}
	require.NoError(t, ValidateGenesis(state))
}

func TestValidateGenesisRequiresSnapshotsForFrozenElectorateVotes(t *testing.T) {
	voter := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	state := DefaultGenesisState()
	state.Proposals = Proposals{{ProposalId: 1, Status: StatusVotingPeriod}}
	state.Votes = Votes{NewVote(1, voter, NewNonSplitVoteOption(OptionYes))}
	state.TallyElectorates = []TallyElectorate{validTallyElectorate(1)}

	err := ValidateGenesis(state)
	require.ErrorContains(t, err, "has no delegation snapshot")

	state.VoteDelegationSnapshots = []VoteDelegationSnapshot{{
		ProposalId: 1,
		Voter:      voter.String(),
	}}
	require.NoError(t, ValidateGenesis(state))
}

func TestValidateGenesisRejectsUninitializedVoteDelegationSnapshotShares(t *testing.T) {
	voter := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	validator := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	state := DefaultGenesisState()
	state.Proposals = Proposals{{ProposalId: 1, Status: StatusVotingPeriod}}
	state.Votes = Votes{NewVote(1, voter, NewNonSplitVoteOption(OptionYes))}
	state.VoteDelegationSnapshots = []VoteDelegationSnapshot{{
		ProposalId: 1,
		Voter:      voter.String(),
		Delegations: []VoteDelegation{{
			Validator: validator.String(),
		}},
	}}

	require.ErrorContains(t, ValidateGenesis(state), "vote delegation snapshot shares are not initialized")
}

func TestValidateGenesisAllowsInactiveBondedStakeInTallyElectorate(t *testing.T) {
	state := DefaultGenesisState()
	state.Proposals = Proposals{{ProposalId: 1, Status: StatusVotingPeriod}}
	state.TallyElectorates = []TallyElectorate{validTallyElectorate(1)}
	state.TallyElectorates[0].TotalBondedTokens = sdk.NewInt(2)

	require.NoError(t, ValidateGenesis(state))

	state.TallyElectorates[0].TotalBondedTokens = sdk.ZeroInt()
	require.ErrorContains(t, ValidateGenesis(state), "exceed total bonded tokens")
}

func TestGenesisStateEqualIncludesVoteDelegationSnapshots(t *testing.T) {
	state1 := GenesisState{VoteDelegationSnapshots: []VoteDelegationSnapshot{{
		ProposalId: 1,
		Voter:      "voter",
		Delegations: []VoteDelegation{{
			Validator: "validator",
			Shares:    sdk.OneDec(),
		}},
	}}}
	state2 := state1
	require.True(t, state1.Equal(state2))

	state2.VoteDelegationSnapshots = nil
	require.False(t, state1.Equal(state2))
}

func TestValidateGenesis(t *testing.T) {
	require.Nil(t, ValidateGenesis(DefaultGenesisState()))
	require.Error(t, ValidateGenesis(&GenesisState{}))
	require.Error(t, ValidateGenesis(nil))
}

func validTallyElectorate(proposalID uint64) TallyElectorate {
	validator := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	return TallyElectorate{
		ProposalId:        proposalID,
		TotalBondedTokens: sdk.OneInt(),
		TallyParams:       DefaultTallyParams(),
		TallyValidators: []TallyValidator{{
			Address:         validator.String(),
			BondedTokens:    sdk.OneInt(),
			DelegatorShares: sdk.OneDec(),
		}},
	}
}
