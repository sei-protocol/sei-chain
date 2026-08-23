package query

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type scanLimitParams struct {
	enforce       bool
	maxLimit      uint64
	maxOffset     uint64
	maxIterations uint64
}

func (p scanLimitParams) checkRequest(req pageRequestNorm) error {
	if !p.enforce {
		return nil
	}
	if req.limit > p.maxLimit {
		return status.Errorf(codes.InvalidArgument,
			"limit %d exceeds the maximum of %d; use key-based pagination instead",
			req.limit, p.maxLimit)
	}
	if req.offset > p.maxOffset {
		return status.Errorf(codes.InvalidArgument,
			"offset %d exceeds the maximum of %d; use key-based pagination instead",
			req.offset, p.maxOffset)
	}
	return nil
}

func scanLimitParamsFromContext(ctx sdk.Context) scanLimitParams {
	if !ctx.IsABCIQuery() {
		return scanLimitParams{}
	}
	limits := ctx.PaginationLimits()
	if !limits.Enforce {
		return scanLimitParams{}
	}
	return scanLimitParams{
		enforce:       true,
		maxLimit:      limits.MaxLimit,
		maxOffset:     limits.MaxOffset,
		maxIterations: limits.MaxIterations,
	}
}

type iterationBudget struct {
	params    scanLimitParams
	count     uint64
	truncated bool
}

func newIterationBudget(params scanLimitParams) *iterationBudget {
	if !params.enforce || params.maxIterations == 0 {
		return nil
	}
	return &iterationBudget{params: params}
}

// begin marks the start of a store iteration. When the flat iteration budget is
// exhausted it returns the resume key and stop=true without consuming the entry.
func (b *iterationBudget) begin(key []byte) (resumeKey []byte, stop bool) {
	if b == nil {
		return nil, false
	}
	if b.count >= b.params.maxIterations {
		b.truncated = true
		return key, true
	}
	b.count++
	return nil, false
}

func (b *iterationBudget) omitTotal() bool {
	return b != nil && b.truncated
}
