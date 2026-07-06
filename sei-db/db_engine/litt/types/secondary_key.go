package types

// A SecondaryKey is used to access specific parts of a value with direct lookups (i.e. without needing to read the
// entire value into memory). It can also be used to alias the entire value to a different key.
type SecondaryKey struct {
	// A key in the DB. Similar to primary keys, secondary keys must be globally unique and cannot be modified after
	// creation (other than being deleted when the TTL expires).
	Key []byte
	// The offset of the start of the byte range described by the secondary key. Must be less than or equal to the
	// length of the full value associated with the key.
	Offset uint32
	// The length of the byte range described by the secondary key. Offset+Length must be less than or equal to the
	// length of the full value associated with the key.
	Length uint32
}
