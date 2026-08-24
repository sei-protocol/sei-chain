package types

// PaginationLimits describes enforced pagination caps on the ABCI query path.
type PaginationLimits struct {
	Enforce       bool
	MaxLimit      uint64
	MaxOffset     uint64
	MaxIterations uint64
}

// NoPaginationLimits disables query-path pagination enforcement.
func NoPaginationLimits() PaginationLimits {
	return PaginationLimits{}
}

// UntrustedPaginationLimits returns enforced limits for untrusted query origins.
func UntrustedPaginationLimits(maxLimit, maxOffset, maxIterations uint64) PaginationLimits {
	return PaginationLimits{
		Enforce:       true,
		MaxLimit:      maxLimit,
		MaxOffset:     maxOffset,
		MaxIterations: maxIterations,
	}
}
