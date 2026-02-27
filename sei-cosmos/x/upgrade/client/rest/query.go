package rest

import (
	"encoding/binary"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/rest"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

func registerQueryRoutes(clientCtx client.Context, r *mux.Router) {
	r.HandleFunc(
		"/upgrade/current", getCurrentPlanHandler(clientCtx),
	).Methods("GET")
	r.HandleFunc(
		"/upgrade/applied/{name}", getDonePlanHandler(clientCtx),
	).Methods("GET")
}

func getCurrentPlanHandler(clientCtx client.Context) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, request *http.Request) {
		// ignore height for now
		res, _, err := clientCtx.Query(fmt.Sprintf("custom/%s/%s", types.QuerierKey, types.QueryCurrent))
		if rest.CheckInternalServerError(w, err) {
			return
		}
		if len(res) == 0 {
			http.NotFound(w, request)
			return
		}

		var plan types.Plan
		err = clientCtx.LegacyAmino.UnmarshalAsJSON(res, &plan)
		if rest.CheckInternalServerError(w, err) {
			return
		}

		rest.PostProcessResponse(w, clientCtx, plan)
	}
}

func getDonePlanHandler(clientCtx client.Context) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["name"]

		params := types.QueryAppliedPlanRequest{Name: name}
		bz, err := clientCtx.LegacyAmino.MarshalAsJSON(params)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		res, _, err := clientCtx.QueryWithData(fmt.Sprintf("custom/%s/%s", types.QuerierKey, types.QueryApplied), bz)
		if rest.CheckBadRequestError(w, err) {
			return
		}

		if len(res) == 0 {
			http.NotFound(w, r)
			return
		}
		if len(res) != 8 {
			rest.WriteErrorResponse(w, http.StatusInternalServerError, "unknown format for applied-upgrade")
			return
		}

		applied := int64(binary.BigEndian.Uint64(res)) //nolint:gosec // stored by SetDone from block heights which are always non-negative
		rest.PostProcessResponse(w, clientCtx, applied)
	}
}
