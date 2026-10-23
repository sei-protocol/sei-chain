package mutation

// A Mutation describes a change to a key-value pair that resulted from block execution.
type Mutation struct {
	// The key in raw form (i.e. containing the module and any other prefixes).
	Key []byte

	// The previous value in raw form. If nil, then there is no previous value.
	PreviousValue []byte

	// The new value in raw form. If nil, then this Mutation signals a deletion.
	NewValue []byte
}
