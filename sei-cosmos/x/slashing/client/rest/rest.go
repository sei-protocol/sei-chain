package rest

import (
	"github.com/gorilla/mux"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/rest"
)

func RegisterHandlers(clientCtx client.Context, rtr *mux.Router) {
	r := rest.WithHTTPDeprecationHeaders(rtr)

	registerQueryRoutes(clientCtx, r)
	registerTxHandlers(clientCtx, r)
}
