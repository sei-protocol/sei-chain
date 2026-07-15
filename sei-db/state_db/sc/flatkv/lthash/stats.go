package lthash

import (
	"encoding/binary"
	"fmt"
)

// moduleStatsEncodedLen is the fixed on-disk size of a marshaled ModuleStats:
// two big-endian int64s (KeyCount || Bytes).
const moduleStatsEncodedLen = 16

// ModuleStats is auxiliary per-(DB, module) metadata accumulated alongside the
// lattice hash: the number of live keys and their total serialized footprint
// (physical key bytes + serialized value bytes) for that module within a DB.
//
// Both are net running totals maintained with the same key-membership rule the
// lattice hash uses (see foldChunk): an add increments KeyCount and adds
// key+value bytes; an update leaves KeyCount unchanged and adjusts Bytes by the
// value-size delta; a delete decrements KeyCount and subtracts the old
// key+value bytes. They are consensus-irrelevant (not folded into the AppHash)
// but are persisted and crash-recovered the same way as the per-module hash.
type ModuleStats struct {
	KeyCount int64
	Bytes    int64
}

// Add returns the sum of two stats. Used to fold a per-block delta into a
// running total (or to aggregate per-module stats into a per-DB total).
func (s ModuleStats) Add(d ModuleStats) ModuleStats {
	return ModuleStats{KeyCount: s.KeyCount + d.KeyCount, Bytes: s.Bytes + d.Bytes}
}

// Marshal encodes stats as 16 big-endian bytes (KeyCount || Bytes).
func (s ModuleStats) Marshal() []byte {
	b := make([]byte, moduleStatsEncodedLen)
	binary.BigEndian.PutUint64(b[0:8], uint64(s.KeyCount))
	binary.BigEndian.PutUint64(b[8:16], uint64(s.Bytes))
	return b
}

// UnmarshalModuleStats decodes bytes produced by ModuleStats.Marshal.
func UnmarshalModuleStats(b []byte) (ModuleStats, error) {
	if len(b) != moduleStatsEncodedLen {
		return ModuleStats{}, fmt.Errorf("lthash: invalid module stats length: got %d, want %d", len(b), moduleStatsEncodedLen)
	}
	return ModuleStats{
		KeyCount: int64(binary.BigEndian.Uint64(b[0:8])),  //nolint:gosec // round-trips the value written by Marshal
		Bytes:    int64(binary.BigEndian.Uint64(b[8:16])), //nolint:gosec // round-trips the value written by Marshal
	}, nil
}
