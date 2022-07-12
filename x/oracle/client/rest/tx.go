package rest

import (
	"fmt"
	"net/http"

	"github.com/sei-protocol/sei-chain/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/rest"

	"github.com/gorilla/mux"
)

func registerTxHandlers(cliCtx client.Context, rtr *mux.Router) {
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/feeder", RestVoter), newDelegateHandlerFunction(cliCtx)).Methods("POST")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/aggregate_prevote", RestVoter), newAggregatePrevoteHandlerFunction(cliCtx)).Methods("POST")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/aggregate_vote", RestVoter), newAggregateVoteHandlerFunction(cliCtx)).Methods("POST")
	rtr.HandleFunc(fmt.Sprintf("/oracle/voters/{%s}/aggregate_combined_vote", RestVoter), newAggregateCombinedVoteHandlerFunction(cliCtx)).Methods("POST")
}

type (
	delegateReq struct {
		BaseReq rest.BaseReq   `json:"base_req" yaml:"base_req"`
		Feeder  sdk.AccAddress `json:"feeder" yaml:"feeder"`
	}

	aggregatePrevoteReq struct {
		BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

		Hash          string `json:"hash" yaml:"hash"`
		ExchangeRates string `json:"exchange_rates" yaml:"exchange_rates"`
		Salt          string `json:"salt" yaml:"salt"`
	}

	aggregateVoteReq struct {
		BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

		ExchangeRates string `json:"exchange_rates" yaml:"exchange_rates"`
		Salt          string `json:"salt" yaml:"salt"`
	}

	aggregateCombinedVoteReq struct {
		BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

		VoteExchangeRates    string `json:"vote_exchange_rates" yaml:"vote_exchange_rates"`
		VoteSalt             string `json:"vote_salt" yaml:"vote_salt"`
		PrevoteHash          string `json:"prevote_hash" yaml:"prevote_hash"`
		PrevoteExchangeRates string `json:"prevote_exchange_rates" yaml:"prevote_exchange_rates"`
		PrevoteSalt          string `json:"prevote_salt" yaml:"prevote_salt"`
	}
)

func newDelegateHandlerFunction(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req delegateReq
		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		// create the message
		msg := types.NewMsgDelegateFeedConsent(voterAddr, req.Feeder)
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}

func newAggregatePrevoteHandlerFunction(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req aggregatePrevoteReq
		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		feederAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		var hash types.AggregateVoteHash

		// If hash is not given, then retrieve hash from exchange_rate and salt
		//nolint:gocritic // ignore for now
		if len(req.Hash) == 0 && (len(req.ExchangeRates) > 0 && len(req.Salt) > 0) {
			_, err := types.ParseExchangeRateTuples(req.ExchangeRates)
			if err != nil {
				rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
				return
			}

			hash = types.GetAggregateVoteHash(req.Salt, req.ExchangeRates, voterAddr)
		} else if len(req.Hash) > 0 {
			hash, err = types.AggregateVoteHashFromHexString(req.Hash)
			if err != nil {
				rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
		} else {
			rest.WriteErrorResponse(w, http.StatusBadRequest, "must provide Hash or (ExchangeRates & Salt)")
			return
		}

		// create the message
		msg := types.NewMsgAggregateExchangeRatePrevote(hash, feederAddr, voterAddr)
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}

func newAggregateVoteHandlerFunction(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req aggregateVoteReq
		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		feederAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		// Check validation of tuples
		_, err = types.ParseExchangeRateTuples(req.ExchangeRates)
		if err != nil {
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		// create the message
		msg := types.NewMsgAggregateExchangeRateVote(req.Salt, req.ExchangeRates, feederAddr, voterAddr)
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}

func newAggregateCombinedVoteHandlerFunction(clientCtx client.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req aggregateCombinedVoteReq
		if !rest.ReadRESTReq(w, r, clientCtx.LegacyAmino, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		feederAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		voterAddr, ok := checkVoterAddressVar(w, r)
		if !ok {
			return
		}

		// Check validation of tuples
		_, err = types.ParseExchangeRateTuples(req.VoteExchangeRates)
		if err != nil {
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		var prevoteHash types.AggregateVoteHash

		// If hash is not given, then retrieve hash from exchange_rate and salt
		if len(req.PrevoteHash) == 0 && (len(req.PrevoteExchangeRates) > 0 && len(req.PrevoteSalt) > 0) { //nolint:gocritic // ignore for now (possible performance gains here)
			_, err := types.ParseExchangeRateTuples(req.PrevoteExchangeRates)
			if err != nil {
				rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
				return
			}

			prevoteHash = types.GetAggregateVoteHash(req.PrevoteSalt, req.PrevoteExchangeRates, voterAddr)
		} else if len(req.PrevoteHash) > 0 {
			prevoteHash, err = types.AggregateVoteHashFromHexString(req.PrevoteHash)
			if err != nil {
				rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
		} else {
			rest.WriteErrorResponse(w, http.StatusBadRequest, "must provide Hash or (ExchangeRates & Salt)")
			return
		}

		msg := types.NewMsgAggregateExchangeRateCombinedVote(req.VoteSalt, req.VoteExchangeRates, prevoteHash, feederAddr, voterAddr)
		if rest.CheckBadRequestError(w, msg.ValidateBasic()) {
			return
		}

		tx.WriteGeneratedTxResponse(clientCtx, w, req.BaseReq, msg)
	}
}
