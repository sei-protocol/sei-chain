package keeper_test

import (
	"errors"
	"fmt"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types"
)

func (suite *KeeperTestSuite) TestGetSetProposal() {
	testcases := map[string]struct {
		proposal    types.Content
		isExpedited bool
	}{
		"regular proposal": {},
		"expedited proposal": {
			isExpedited: true,
		},
	}

	for _, tc := range testcases {
		tp := TestProposal
		proposal, err := suite.app.GovKeeper.SubmitProposalWithExpedite(suite.ctx, tp, tc.isExpedited)
		suite.Require().NoError(err)
		proposalID := proposal.ProposalId
		suite.app.GovKeeper.SetProposal(suite.ctx, proposal)

		gotProposal, ok := suite.app.GovKeeper.GetProposal(suite.ctx, proposalID)
		suite.Require().True(ok)
		suite.Require().True(proposal.Equal(gotProposal))
	}
}

func (suite *KeeperTestSuite) TestActivateVotingPeriod() {
	testcases := []struct {
		name        string
		proposal    types.Content
		isExpedited bool
	}{
		{
			name:        "expedited",
			isExpedited: true,
		},
		{
			name: "not expedited",
		},
	}
	for _, tc := range testcases {
		suite.T().Run(tc.name, func(t *testing.T) {
			tp := TestProposal
			proposal, err := suite.app.GovKeeper.SubmitProposalWithExpedite(suite.ctx, tp, tc.isExpedited)
			suite.Require().NoError(err)

			suite.Require().True(proposal.VotingStartTime.Equal(time.Time{}))

			suite.app.GovKeeper.ActivateVotingPeriod(suite.ctx, proposal)

			suite.Require().True(proposal.VotingStartTime.Equal(suite.ctx.BlockHeader().Time))
			proposal, ok := suite.app.GovKeeper.GetProposal(suite.ctx, proposal.ProposalId)
			suite.Require().True(ok)

			activeIterator := suite.app.GovKeeper.ActiveProposalQueueIterator(suite.ctx, proposal.VotingEndTime)
			suite.Require().True(activeIterator.Valid())

			proposalID := types.GetProposalIDFromBytes(activeIterator.Value())
			suite.Require().Equal(proposalID, proposal.ProposalId)
			require.NoError(t, activeIterator.Close())

			votingParams := suite.app.GovKeeper.GetVotingParams(suite.ctx)

			if tc.isExpedited {
				require.Equal(t, proposal.VotingEndTime, proposal.VotingStartTime.Add(votingParams.ExpeditedVotingPeriod))
			} else {
				require.Equal(t, proposal.VotingEndTime, proposal.VotingStartTime.Add(votingParams.VotingPeriod))
			}

			// teardown
			suite.app.GovKeeper.RemoveFromActiveProposalQueue(suite.ctx, proposalID, proposal.VotingEndTime)
			suite.app.GovKeeper.DeleteProposal(suite.ctx, proposalID)
		})
	}
}

type invalidProposalRoute struct{ types.TextProposal }

func (invalidProposalRoute) ProposalRoute() string { return "nonexistingroute" }

func (suite *KeeperTestSuite) TestSubmitProposal() {
	testCases := []struct {
		content     types.Content
		expectedErr error
	}{
		{&types.TextProposal{Title: "title", Description: "description"}, nil},
		// Keeper does not check the validity of title and description, no error
		{&types.TextProposal{Title: "", Description: "description"}, nil},
		{&types.TextProposal{Title: strings.Repeat("1234567890", 100), Description: "description"}, nil},
		{&types.TextProposal{Title: "title", Description: ""}, nil},
		{&types.TextProposal{Title: "title", Description: strings.Repeat("1234567890", 1000)}, nil},
		// error only when invalid route
		{&invalidProposalRoute{}, types.ErrNoProposalHandlerExists},
	}

	for i, tc := range testCases {
		_, err := suite.app.GovKeeper.SubmitProposal(suite.ctx, tc.content)
		suite.Require().True(errors.Is(tc.expectedErr, err), "tc #%d; got: %v, expected: %v", i, err, tc.expectedErr)
	}
}

func (suite *KeeperTestSuite) TestGetProposalsFiltered() {
	proposalID := uint64(1)
	status := []types.ProposalStatus{types.StatusDepositPeriod, types.StatusVotingPeriod}

	addr1 := sdk.AccAddress("foo_________________")

	for _, s := range status {
		for i := 0; i < 50; i++ {
			p, err := types.NewProposal(TestProposal, proposalID, time.Now(), time.Now(), false)
			suite.Require().NoError(err)

			p.Status = s

			if i%2 == 0 {
				d := types.NewDeposit(proposalID, addr1, nil)
				v := types.NewVote(proposalID, addr1, types.NewNonSplitVoteOption(types.OptionYes))
				suite.app.GovKeeper.SetDeposit(suite.ctx, d)
				suite.app.GovKeeper.SetVote(suite.ctx, v)
			}

			suite.app.GovKeeper.SetProposal(suite.ctx, p)
			proposalID++
		}
	}

	testCases := []struct {
		params             types.QueryProposalsParams
		expectedNumResults int
	}{
		{types.NewQueryProposalsParams(1, 50, types.StatusNil, nil, nil), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusDepositPeriod, nil, nil), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusVotingPeriod, nil, nil), 50},
		{types.NewQueryProposalsParams(1, 25, types.StatusNil, nil, nil), 25},
		{types.NewQueryProposalsParams(2, 25, types.StatusNil, nil, nil), 25},
		{types.NewQueryProposalsParams(1, 50, types.StatusRejected, nil, nil), 0},
		{types.NewQueryProposalsParams(1, 50, types.StatusNil, addr1, nil), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusNil, nil, addr1), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusNil, addr1, addr1), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusDepositPeriod, addr1, addr1), 25},
		{types.NewQueryProposalsParams(1, 50, types.StatusDepositPeriod, nil, nil), 50},
		{types.NewQueryProposalsParams(1, 50, types.StatusVotingPeriod, nil, nil), 50},
	}

	for i, tc := range testCases {
		suite.Run(fmt.Sprintf("Test Case %d", i), func() {
			proposals := suite.app.GovKeeper.GetProposalsFiltered(suite.ctx, tc.params)
			suite.Require().Len(proposals, tc.expectedNumResults)

			for _, p := range proposals {
				if types.ValidProposalStatus(tc.params.ProposalStatus) {
					suite.Require().Equal(tc.params.ProposalStatus, p.Status)
				}
			}
		})
	}
}

func (suite *KeeperTestSuite) TestParamChangeProposal() {
	// Simple parameter changes
	validStakingChange := proposal.ParamChange{
		Subspace: "staking",
		Key:      "MaxValidators",
		Value:    "100",
	}

	invalidStakingChange := proposal.ParamChange{
		Subspace: "staking",
		Key:      "NonExistentParam",
		Value:    "100",
	}

	// Complex gov parameter changes
	validVotingParamsChange := proposal.ParamChange{
		Subspace: "gov",
		Key:      "votingparams",
		Value:    `{"voting_period":"172800s","expedited_voting_period":"86400s"}`,
	}

	invalidVotingParamsChange := proposal.ParamChange{
		Subspace: "gov",
		Key:      "invalidvotingparams",
		Value:    `{"voting_period":"invalid"}`, // Invalid duration
	}

	testCases := map[string]struct {
		proposal    types.Content
		expectError bool
	}{
		"valid staking param change": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Valid Staking Param Change",
				Description: "Testing valid staking param changes",
				Changes:     []proposal.ParamChange{validStakingChange},
				IsExpedited: false,
			},
			expectError: false,
		},
		"invalid staking param change": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Invalid Staking Param Change",
				Description: "Testing invalid staking param changes",
				Changes:     []proposal.ParamChange{invalidStakingChange},
				IsExpedited: false,
			},
			expectError: true,
		},
		"valid voting params change": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Valid Voting Params Change",
				Description: "Testing valid voting param changes",
				Changes:     []proposal.ParamChange{validVotingParamsChange},
				IsExpedited: false,
			},
			expectError: false,
		},
		"invalid voting params change": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Invalid Voting Params Change",
				Description: "Testing invalid voting param changes",
				Changes:     []proposal.ParamChange{invalidVotingParamsChange},
				IsExpedited: false,
			},
			expectError: true,
		},
		"mixed valid and invalid params": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Mixed Param Changes",
				Description: "Testing both valid and invalid params",
				Changes:     []proposal.ParamChange{validStakingChange, invalidStakingChange},
				IsExpedited: false,
			},
			expectError: true,
		},
		"multiple valid params": {
			proposal: &proposal.ParameterChangeProposal{
				Title:       "Multiple Valid Params",
				Description: "Testing multiple valid param changes",
				Changes:     []proposal.ParamChange{validStakingChange, validVotingParamsChange},
				IsExpedited: false,
			},
			expectError: false,
		},
	}

	for name, tc := range testCases {
		suite.Run(name, func() {
			_, err := suite.app.GovKeeper.SubmitProposal(suite.ctx, tc.proposal)
			if tc.expectError {
				suite.Require().Error(err)
				suite.Require().Contains(err.Error(), "parameter")
			} else {
				suite.Require().NoError(err)
			}
		})
	}
}
