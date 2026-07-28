package segment

// SegmentVersion is used to indicate the serialization version of a segment. Whenever serialization formats change
// in segment files, this version should be incremented.
//
// Versions 0, 1, and 2 are no longer supported and have been removed from the codebase. The current code can only
// read and write segments at LatestSegmentVersion. The constant numbers below are kept implicitly (no constant is
// declared for them) so that LatestSegmentVersion still increases monotonically as a historical record.
type SegmentVersion uint32

const (
	// ShardedAddressSegmentVersion is the current on-disk format. It defines:
	//   - The 13-byte sharded Address layout in the key file (index, offset, shardID, valueSize). The
	//     keymap stores the same layout.
	//   - No per-segment hashing salt in the metadata file; shards are assigned to values in round-robin
	//     order at write time, which makes the key->shard mapping unpredictable to outside callers
	//     without needing a hash function or any randomness in the metadata.
	//   - No per-value length prefix in value files. The length lives only in the Address that points
	//     at the value, which lets secondary keys alias sub-ranges of a value without duplicating data.
	//   - Per-record `| kind(u8) | keyLen(u16) | key | address(13) |` layout in the key file. The kind
	//     byte distinguishes primary keys from secondary keys and marks group boundaries used at
	//     recovery time to discard torn writes atomically. Key length is capped at 64 KiB.
	//
	// The constant name predates the value-file and key-file changes; it is retained because no
	// instance of this codebase has been deployed to production, so there is no compatibility cost to
	// folding the new format into the same version number rather than bumping it.
	ShardedAddressSegmentVersion SegmentVersion = 3

	// CompressedSegmentVersion adds a single compression-algorithm byte to the end of the metadata file
	// (see V4MetadataSize). The key-file and value-file layouts are unchanged from
	// ShardedAddressSegmentVersion, so a v4 segment written with CompressionNone is byte-compatible with
	// a v3 segment; the version bump exists only to make the wider metadata format explicit and to keep
	// reads of pre-existing v3 metadata files working.
	CompressedSegmentVersion SegmentVersion = 4
)

// LatestSegmentVersion always refers to the latest version of the segment serialization format.
const LatestSegmentVersion = CompressedSegmentVersion
