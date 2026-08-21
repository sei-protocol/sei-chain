package query

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type scanLimitParams struct {
	enforce bool
	limit   uint64
	// boundRequest rejects PageRequest limit and offset above `limit` before
	// any store iteration.
	boundRequest bool
}

func v66ScanLimitParams() scanLimitParams {
	return scanLimitParams{enforce: true, limit: MaxScanLimit}
}

func scanLimitParamsFromContext(ctx sdk.Context) scanLimitParams {
	if !ctx.IsABCIQuery() {
		return scanLimitParams{}
	}
	if !ctx.EnforceQueryScanLimit() {
		return scanLimitParams{}
	}
	return scanLimitParams{enforce: true, limit: ctx.QueryScanLimit(), boundRequest: true}
}

// checkRequest rejects limit and offset that cannot be served within `limit`.
func (p scanLimitParams) checkRequest(req pageRequestNorm) error {
	if !p.boundRequest || p.limit == 0 {
		return nil
	}
	if req.limit > p.limit {
		return status.Errorf(codes.InvalidArgument,
			"limit %d exceeds the maximum of %d; use key-based pagination instead",
			req.limit, p.limit)
	}
	if req.offset > p.limit {
		return status.Errorf(codes.InvalidArgument,
			"offset %d exceeds the maximum of %d; use key-based pagination instead",
			req.offset, p.limit)
	}
	return nil
}

func (p scanLimitParams) checkKeyPath(totalIter uint64) error {
	if p.enforce && totalIter > p.limit {
		return scanLimitError(p.limit, "use a more specific key prefix or reduce limit")
	}
	return nil
}

func (p scanLimitParams) checkPostPage(pageCompleteIter uint64, countTotal bool) (stop bool, err error) {
	if p.enforce && pageCompleteIter > p.limit {
		if countTotal {
			return false, scanLimitError(p.limit, "use key-based pagination instead")
		}
		return true, nil
	}
	return false, nil
}

func scanLimitError(limit uint64, hint string) error {
	return status.Errorf(codes.InvalidArgument,
		"scanned more than %d entries without filling the page; %s", limit, hint)
}
