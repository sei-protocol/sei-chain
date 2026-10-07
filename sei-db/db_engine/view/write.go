package view

// Write is one key's change: a value to store, or a deletion.
//
// A nil Value is a deletion. That is the manager's tombstone convention throughout — BatchUpdater
// returns nil from NewValueFor to delete, and the version diff maps hold nil for a deleted key — so a
// caller with a genuinely empty value passes a non-nil, zero-length slice.
//
// The manager never retains Key, so it may be carved from a shared buffer.
type Write struct {
	// Key is the key to write.
	Key string

	// Value is the value to store, or nil to delete the key.
	Value []byte
}
