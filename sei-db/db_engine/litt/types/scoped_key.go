package types

// ScopedKey is a key, plus additional information about the value associated with the key.
type ScopedKey struct {
	// A key in the DB.
	Key []byte
	// The location where the value associated with the key is stored.
	Address Address
	// The length of the value associated with the key.
	ValueSize uint32
}
