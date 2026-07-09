package indexer

// maxBoundedPrealloc caps how much a bounded search fast path preallocates for
// its result slice, so a very large (or disabled) limit does not eagerly
// allocate.
const maxBoundedPrealloc = 4096

// BoundedCap returns a sensible initial capacity for a result slice given a
// limit. A non-positive limit means "unbounded", in which case we let the
// slice grow on demand.
func BoundedCap(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit < maxBoundedPrealloc {
		return limit
	}
	return maxBoundedPrealloc
}

// HeightInRange reports whether height h falls within the (already
// inclusivity-adjusted) bounds of a numeric height query range.
func HeightInRange(h int64, qr QueryRange) bool {
	if lower := qr.LowerBoundValue(); lower != nil {
		if lb, ok := lower.(int64); ok && h < lb {
			return false
		}
	}
	if upper := qr.UpperBoundValue(); upper != nil {
		if ub, ok := upper.(int64); ok && h > ub {
			return false
		}
	}
	return true
}

// ScanBudget bounds the number of index entries a fallback scan may examine
// across all passes of a single query (one query = one shared budget). It
// counts work, not output: it is stepped once per iterator advance on the
// fallback scan path and not for fast-path driver scans or point-probes. A
// non-positive limit disables the budget.
type ScanBudget struct {
	limit int
	used  int
}

// NewScanBudget returns a ScanBudget with the given entry limit (<= 0 disables).
func NewScanBudget(limit int) *ScanBudget {
	return &ScanBudget{limit: limit}
}

// Step accounts for one examined entry and returns ErrSearchScanBudgetExceeded
// once the cumulative count exceeds the limit. It is safe to call on a nil
// budget (treated as unlimited).
func (b *ScanBudget) Step() error {
	if b == nil || b.limit <= 0 {
		return nil
	}
	b.used++
	if b.used > b.limit {
		return ErrSearchScanBudgetExceeded
	}
	return nil
}

// Used reports the number of entries examined so far.
func (b *ScanBudget) Used() int {
	if b == nil {
		return 0
	}
	return b.used
}

// PrefixUpperBound returns the exclusive end key for iterating over prefix,
// i.e. the smallest key strictly greater than every key having the prefix.
// It returns nil when prefix is empty or all bytes are 0xFF (no upper bound).
func PrefixUpperBound(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}
