package segment

// SegmentVersion is used to indicate the serialization version of a segment. Whenever serialization formats change
// in segment files, this version should be incremented.
type SegmentVersion uint32

const (
	// OldHashFunctionSegmentVersion is the serialization version for the old hash function.
	OldHashFunctionSegmentVersion SegmentVersion = 0

	// SipHashSegmentVersion is the version when the siphash hash function was introduced for sharding.
	SipHashSegmentVersion SegmentVersion = 1

	// ValueSizeSegmentVersion adds the length of values to the key file. Previously, only the key and the address were
	// stored in the key file. It also adds the key count to the segment metadata file.
	ValueSizeSegmentVersion SegmentVersion = 2
)

// LatestSegmentVersion always refers to the latest version of the segment serialization format.
const LatestSegmentVersion = ValueSizeSegmentVersion
