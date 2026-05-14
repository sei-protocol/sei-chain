//go:build littdb_wip

package segment

// SegmentVersion is used to indicate the serialization version of a segment. Whenever serialization formats change
// in segment files, this version should be incremented.
//
// Versions 0, 1, and 2 are no longer supported and have been removed from the codebase. The current code can only
// read and write segments at LatestSegmentVersion. The constant numbers below are kept implicitly (no constant is
// declared for them) so that LatestSegmentVersion still increases monotonically as a historical record.
type SegmentVersion uint32

const (
	// ShardedAddressSegmentVersion is the on-disk format that:
	//   - Replaces the legacy 8-byte address + separate value size in the key file with the 13-byte sharded
	//     Address layout (index, offset, shardID, valueSize). The keymap stores the same layout.
	//   - Drops the per-segment hashing salt from the metadata file. Shards are assigned to values in
	//     round-robin order at write time, which makes the key->shard mapping unpredictable to outside
	//     callers without needing a hash function or any randomness in the metadata.
	ShardedAddressSegmentVersion SegmentVersion = 3
)

// LatestSegmentVersion always refers to the latest version of the segment serialization format.
const LatestSegmentVersion = ShardedAddressSegmentVersion
