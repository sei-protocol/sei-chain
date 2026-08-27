package types

import (
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
