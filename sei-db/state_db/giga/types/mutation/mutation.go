package mutation

// A Mutation describes a change to a key-value pair that resulted from block execution.
type Mutation struct {
	// The key in raw form (i.e. containing the module and any other prefixes).
	Key []byte

	// If true, the previous value is unknown. This is a temporary flag designed to support migration from legacy
	// data formats which do not store previous values. Eventually, support for unknown previous values will be removed.
	PreviousValueUnknown bool

	// The previous value in raw form. If nil, then there is no previous value.
	// Undefined if PreviousValueUnknown is true.
	PreviousValue []byte

	// The new value in raw form. If nil, then this Mutation signals a deletion.
	NewValue []byte
}
