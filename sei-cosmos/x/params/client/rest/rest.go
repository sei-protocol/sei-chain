package rest

import (
	"net/http"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/rest"
	govrest "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/client/rest"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramscutils "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/client/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
)

// ProposalRESTHandler returns a ProposalRESTHandler that exposes the param
// change REST handler with a given sub-route.
func ProposalRESTHandler(clientCtx client.Context) govrest.ProposalRESTHandler {
	return govrest.ProposalRESTHandler{
		SubRoute: "param_change",
		Handler:  postProposalHandlerFn(clientCtx),
	}
}

func postProposalHandlerFn(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req paramscutils.ParamChangeProposalReq
		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		isExpedited := req.IsExpedited
		content := proposal.NewParameterChangeProposal(req.Title, req.Description, req.Changes.ToParamChanges(), isExpedited)

		msg, err := govtypes.NewMsgSubmitProposalWithExpedite(content, req.Deposit, req.Proposer, isExpedited)
		if rest.CheckBadRequestError(w, err) {
			return
		}
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}
