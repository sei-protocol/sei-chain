package types

// A request to put a key-value pair with optional secondary keys into the database.
type PutRequest struct {
	// Key is the primary key.
	Key []byte
	// Value is the value to put. Only written once, even if secondary keys are provided.
	Value []byte
	// Secondary keys pointing to sub-ranges of the value. May be nil.
	SecondaryKeys []*SecondaryKey
}
