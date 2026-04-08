// VType is short for "value type". ValueTypes encapsulate values in FlatKV, and are responsible
// for handling serialization, deserialization, and multi-field encapsulation.
package vtype

// All values in FlatKV are serialized/deserialzed using a VType (except for the metadata table).
//
// VTypes should be well-behaved when nil, and it should be safe to call into them without checking for nil.
// Nil VTypes should identify themselves as deletion operations with all zero values.
type VType interface {
	// Serialize the value to a byte slice.
	Serialize() []byte

	// IsDelete returns true if the value is a deletion operation.
	IsDelete() bool
}
