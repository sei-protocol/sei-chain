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
