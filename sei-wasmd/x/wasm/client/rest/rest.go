package rest

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/gorilla/mux"
)

// RegisterRoutes registers staking-related REST handlers to a router
func RegisterRoutes(cliCtx client.Context, r *mux.Router) {
	registerQueryRoutes(cliCtx, r)
	registerTxRoutes(cliCtx, r)
	registerNewTxRoutes(cliCtx, r)
}
