package simulation

import (
	seiappparams "github.com/sei-protocol/sei-chain/app/params"
	simtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/simulation"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/simulation"
)

// OpWeightSubmitParamChangeProposal app params key for param change proposal
const OpWeightSubmitParamChangeProposal = "op_weight_submit_param_change_proposal"

// ProposalContents defines the module weighted proposals' contents
func ProposalContents(paramChanges []simtypes.ParamChange) []simtypes.WeightedProposalContent {
	return []simtypes.WeightedProposalContent{
		simulation.NewWeightedProposalContent(
			OpWeightSubmitParamChangeProposal,
			seiappparams.DefaultWeightParamChangeProposal,
			SimulateParamChangeProposalContent(paramChanges),
		),
	}
}
