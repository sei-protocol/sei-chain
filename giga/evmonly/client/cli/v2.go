package cli

import (
	"encoding/json"
	"strings"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// Flags of V2's gov, upgrade and query commands.
const (
	flagTitle         = "title"
	flagDescription   = "description"
	flagIsExpedited   = "is-expedited"
	flagProposalType  = "type"
	flagDeposit       = "deposit"
	flagProposal      = "proposal"
	flagUpgradeHeight = "upgrade-height"
	flagUpgradeInfo   = "upgrade-info"
	flagStatus        = "status"
	flagVoter         = "voter"
	flagDepositor     = "depositor"
)

// proposalFlags are the submit-proposal flags a --proposal file replaces.
var proposalFlags = []string{flagTitle, flagDescription, flagProposalType, flagDeposit}

// v2ParamChange is one change of a V2 param-change proposal file.
type v2ParamChange struct {
	Subspace string          `json:"subspace"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
}

// v2ParamChangeProposal is a V2 param-change proposal file.
type v2ParamChangeProposal struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	IsExpedited bool            `json:"is_expedited"`
	Changes     []v2ParamChange `json:"changes"`
	Deposit     string          `json:"deposit"`
}

// normalizeVoteOption maps V2's vote option spellings to VoteOption names.
func normalizeVoteOption(option string) string {
	switch option {
	case "Yes", "yes":
		return govtypes.OptionYes.String()
	case "Abstain", "abstain":
		return govtypes.OptionAbstain.String()
	case "No", "no":
		return govtypes.OptionNo.String()
	case "NoWithVeto", "no_with_veto":
		return govtypes.OptionNoWithVeto.String()
	default:
		return option
	}
}

// normalizeWeightedVoteOptions normalizes each option of a V2 weighted vote,
// giving an option without a weight a weight of 1.
func normalizeWeightedVoteOptions(options string) string {
	parts := strings.Split(options, ",")
	out := make([]string, 0, len(parts))
	for _, option := range parts {
		fields := strings.Split(option, "=")
		fields[0] = normalizeVoteOption(fields[0])
		if len(fields) < 2 {
			fields = append(fields, "1")
		}
		out = append(out, strings.Join(fields, "="))
	}
	return strings.Join(out, ",")
}

// normalizeProposalType maps V2's text proposal spellings to its type name, and
// any other type to "".
func normalizeProposalType(proposalType string) string {
	switch proposalType {
	case "Text", "text":
		return govtypes.ProposalTypeText
	default:
		return ""
	}
}

// normalizeProposalStatus maps V2's status spellings to ProposalStatus names.
func normalizeProposalStatus(status string) string {
	switch status {
	case "DepositPeriod", "deposit_period":
		return govtypes.StatusDepositPeriod.String()
	case "VotingPeriod", "voting_period":
		return govtypes.StatusVotingPeriod.String()
	case "Passed", "passed":
		return govtypes.StatusPassed.String()
	case "Rejected", "rejected":
		return govtypes.StatusRejected.String()
	case "Failed", "failed":
		return govtypes.StatusFailed.String()
	default:
		return status
	}
}
