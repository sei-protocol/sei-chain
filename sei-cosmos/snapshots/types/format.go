package types

const (
	// FormatV1 identifies version 1 of multistore snapshots.
	FormatV1 uint32 = 1
	// CurrentFormat is the currently used format for snapshots. Snapshots using the same format
	// must be identical across all nodes for a given height, so this must be bumped when the binary
	// snapshot output changes.
	CurrentFormat uint32 = 2
)

// SupportedFormats returns the multistore snapshot formats this binary can restore from.
func SupportedFormats() []uint32 {
	return []uint32{FormatV1, CurrentFormat}
}
