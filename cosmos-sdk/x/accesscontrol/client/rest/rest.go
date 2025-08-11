package rest

import (
	"net/http"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client/tx"

	govrest "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/client/rest"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/types/accesscontrol"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/types/rest"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/accesscontrol/types"
	govtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/types"
)

// PlanRequest defines a proposal for a new upgrade plan.
type UpdateResourceDependencyMappingRequest struct {
	BaseReq                  rest.BaseReq                             `json:"base_req" yaml:"base_req"`
	Title                    string                                   `json:"title" yaml:"title"`
	Description              string                                   `json:"description" yaml:"description"`
	Deposit                  sdk.Coins                                `json:"deposit" yaml:"deposit"`
	MessageDependencyMapping []accesscontrol.MessageDependencyMapping `json:"message_dependency_mapping" yaml:"message_dependency_mapping"`
}

func UpdateResourceDependencyProposalRESTHandler(clientCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "update_resource_dependency_mapping",
		Handler:  newUpdateResourceDependencyPostPlanHandler(clientCtx),
	}
}

func newUpdateResourceDependencyPostPlanHandler(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdateResourceDependencyMappingRequest

		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		fromAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		content := types.NewMsgUpdateResourceDependencyMappingProposal(
			req.Title, req.Description, req.MessageDependencyMapping,
		)
		msg, err := govtypes.NewMsgSubmitProposal(content, req.Deposit, fromAddr)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}
