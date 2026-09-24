package retiredoracle

import (
	"strings"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	storekeys "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// QueryResponse returns the deprecation response for a retired Oracle raw-store query.
func QueryResponse(requestPath string) *abci.ResponseQuery {
	path := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	if len(path) != 3 || path[0] != "store" || path[1] != storekeys.OracleStoreKey ||
		(path[2] != "key" && path[2] != "subspace") {
		return nil
	}
	response := sdkerrors.QueryResult(ErrDeprecated)
	return &response
}
