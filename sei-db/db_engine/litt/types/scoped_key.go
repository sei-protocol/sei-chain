package types

// ScopedKey is a key paired with the Address that locates its value on disk.
// The value's size and the owning shard are both stored within the Address itself.
type ScopedKey struct {
	// A key in the DB.
	Key []byte
	// The location where the value associated with the key is stored.
	Address Address
	// Kind tags the record's role in the key file: ordinary primary, primary with secondaries to
	// follow, or one of the secondaries that follow such a primary. The zero value
	// (KeyKindStandalone) means an ordinary primary key, so call sites that do not care about
	// secondary keys can construct ScopedKey literals as before.
	Kind KeyKind
}
