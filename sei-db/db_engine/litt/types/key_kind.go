package types

// KeyKind tags each record in the per-segment key file. It distinguishes primary keys from secondary
// keys (which alias sub-ranges of another key's value bytes) and also delimits "groups" written by a
// single Put, used at recovery time to discard torn writes atomically.
//
// Layout on disk: each key-file record begins with a single KeyKind byte. Values 4-255 are reserved
// for future record kinds.
type KeyKind uint8

const (
	// KeyKindStandalone is a primary key whose Put did not include any secondary keys. The zero
	// value is the default so any ScopedKey constructed without an explicit Kind is treated as an
	// ordinary primary key.
	KeyKindStandalone KeyKind = 0
	// KeyKindPrimary is a primary key whose Put included at least one secondary; the secondaries
	// appear contiguously in the key file immediately after this record and terminate with a
	// KeyKindFinalSecondary record.
	KeyKindPrimary KeyKind = 1
	// KeyKindSecondary is a secondary key that is not the last secondary in its group.
	KeyKindSecondary KeyKind = 2
	// KeyKindFinalSecondary is the last secondary in a group; it terminates the group and signals
	// that the group is fully written.
	KeyKindFinalSecondary KeyKind = 3
)

// IsPrimary returns true if the key kind denotes a primary key (a standalone primary or a primary with
// secondaries), and false if it denotes a secondary key.
func (k KeyKind) IsPrimary() bool {
	return k == KeyKindStandalone || k == KeyKindPrimary
}
