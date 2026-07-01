package segment

import (
	"fmt"
	"os"
)

// RollbackToKeyCount truncates a sealed segment so that it retains only its first survivingKeyCount key-file
// records (in the order they were written), discarding every key and value written afterwards.
//
// This is a destructive, offline operation. The caller must guarantee that the database is not running and
// that nothing else is touching the segment's files.
//
// survivingKeyCount counts individual key-file records; a primary key and each of its secondary keys count
// separately. It must not exceed the number of records currently in the segment. To keep the segment
// internally consistent, callers should pass a count that lands on a group boundary (a primary plus all of
// its secondaries are either all kept or all discarded); RollbackToKeyCount itself does not enforce this.
//
// The steps are ordered so that an interruption never leaves a torn record:
//  1. the key file is rewritten via an atomic swap (the commit point),
//  2. each shard's value file is truncated, and
//  3. the segment's key count is recorded in the metadata file.
//
// The surviving records always occupy a contiguous prefix of the key file and of each shard's value file,
// so the addresses of the kept records are never disturbed.
func (s *Segment) RollbackToKeyCount(survivingKeyCount uint32) error {
	if !s.IsSealed() {
		return fmt.Errorf("segment %d is not sealed, cannot roll back", s.index)
	}

	keys, err := s.keys.readKeys()
	if err != nil {
		return fmt.Errorf("failed to read keys for segment %d: %w", s.index, err)
	}

	if int(survivingKeyCount) > len(keys) {
		return fmt.Errorf("surviving key count %d exceeds the %d records in segment %d",
			survivingKeyCount, len(keys), s.index)
	}
	if int(survivingKeyCount) == len(keys) {
		// Nothing was written after the boundary; the segment is already in its target state.
		return nil
	}

	survivingKeys := keys[:survivingKeyCount]

	// 1. Rewrite the key file with only the surviving records. The atomic rename of the swap file over the
	// original key file is the commit point for this segment's rollback.
	swapFile, err := createKeyFile(s.logger, s.index, s.keys.segmentPath, true)
	if err != nil {
		return fmt.Errorf("failed to create swap key file for segment %d: %w", s.index, err)
	}
	for _, key := range survivingKeys {
		if err = swapFile.write(key); err != nil {
			return fmt.Errorf("failed to write key to swap file for segment %d: %w", s.index, err)
		}
	}
	if err = swapFile.seal(); err != nil {
		return fmt.Errorf("failed to seal swap key file for segment %d: %w", s.index, err)
	}
	if err = swapFile.atomicSwap(s.fsync); err != nil {
		return fmt.Errorf("failed to swap key file for segment %d: %w", s.index, err)
	}
	s.keys = swapFile

	// 2. Truncate each shard's value file to the end of its last surviving value. Values carry no length
	// prefix on disk, so a value occupies exactly [offset, offset+valueSize), and the surviving values form
	// a prefix of each shard because values are appended in write order.
	shardEnds := make([]uint64, len(s.shards))
	for _, key := range survivingKeys {
		shardID := key.Address.ShardID()
		if int(shardID) >= len(s.shards) {
			return fmt.Errorf("segment %d has a key with shard ID %d outside its sharding factor %d",
				s.index, shardID, len(s.shards))
		}
		end := uint64(key.Address.Offset()) + uint64(key.Address.ValueSize())
		if end > shardEnds[shardID] {
			shardEnds[shardID] = end
		}
	}
	for shardID, valueFile := range s.shards {
		if err = os.Truncate(valueFile.path(), int64(shardEnds[shardID])); err != nil { //nolint:gosec // value offsets are bounded to 2^32
			return fmt.Errorf("failed to truncate value file for segment %d shard %d: %w", s.index, shardID, err)
		}
	}

	// 3. Record the new key count. The seal time is preserved so the segment's TTL/expiry is unaffected.
	s.metadata.keyCount = survivingKeyCount
	if err = s.metadata.write(); err != nil {
		return fmt.Errorf("failed to update metadata for segment %d: %w", s.index, err)
	}

	return nil
}
