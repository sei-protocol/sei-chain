package query

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type scanLimitParams struct {
	enforce bool
	limit   uint64
}

func scanLimitParamsFromContext(ctx sdk.Context) scanLimitParams {
	if !ctx.IsABCIQuery() {
		return scanLimitParams{enforce: false, limit: 0}
	}
	if !ctx.EnforceQueryScanLimit() {
		return scanLimitParams{enforce: false, limit: 0}
	}
	return scanLimitParams{enforce: true, limit: ctx.QueryScanLimit()}
}

func (p scanLimitParams) checkKeyPath(totalIter uint64) error {
	if p.enforce && totalIter > p.limit {
		return scanLimitError(p.limit, "use a more specific key prefix or reduce limit")
	}
	return nil
}

func (p scanLimitParams) checkOffsetBeforePageFilled(numHits, end, offset, totalIter uint64) error {
	if p.enforce && numHits < end && totalIter > paginationEnd(offset, p.limit) {
		return scanLimitError(p.limit, "use key-based pagination instead")
	}
	return nil
}

func (p scanLimitParams) checkPostPage(pageCompleteIter uint64, countTotal bool) error {
	if p.enforce && pageCompleteIter > p.limit {
		if !countTotal {
			return nil
		}
		return scanLimitError(p.limit, "use key-based pagination instead")
	}
	return nil
}

func scanLimitError(limit uint64, hint string) error {
	return status.Errorf(codes.InvalidArgument,
		"scanned more than %d entries without filling the page; %s", limit, hint)
}
