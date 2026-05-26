package types

// ScopedKey is a key paired with the Address that locates its value on disk.
// The value's size and the owning shard are both stored within the Address itself.
type ScopedKey struct {
	// A key in the DB.
	Key []byte
	// The location where the value associated with the key is stored.
	Address Address
}
