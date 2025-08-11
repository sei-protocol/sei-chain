package rest

import (
	"github.com/gorilla/mux"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/client/rest"
)

func RegisterHandlers(clientCtx client.Context, rtr *mux.Router) {
	r := rest.WithHTTPDeprecationHeaders(rtr)

	registerQueryRoutes(clientCtx, r)
	registerTxHandlers(clientCtx, r)
}
