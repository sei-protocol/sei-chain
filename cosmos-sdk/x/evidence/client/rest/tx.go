package rest

import (
	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"

	"github.com/gorilla/mux"
)

func registerTxRoutes(clientCtx client.Context, r *mux.Router, handlers []EvidenceRESTHandler) {
	// TODO: Register tx handlers.
}
