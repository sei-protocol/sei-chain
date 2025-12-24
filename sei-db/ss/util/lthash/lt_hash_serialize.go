package lthash

import (
	"encoding/binary"
)

// SerializeForLtHash serializes a KV for LtHash.
// Returns nil for empty/zero values.
//
// Format: dbNameLen[2] || dbName || key || value
//
// The dbName prefix isolates hash input domain across stores,
// preventing cross-store collision attacks where different stores
// might have the same (key, value) pair.
//
// Note: EVM data already has type prefixes in keys (0x03=State, 0x07=Code,
// 0x0a=Nonce, etc.), so no additional type prefix is needed here.
func SerializeForLtHash(dbName string, key, value []byte) []byte {
	if len(key) == 0 || len(value) == 0 {
		return nil
	}

	dbNameBytes := []byte(dbName)
	buf := make([]byte, 2+len(dbNameBytes)+len(key)+len(value))
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(dbNameBytes)))
	copy(buf[2:2+len(dbNameBytes)], dbNameBytes)
	copy(buf[2+len(dbNameBytes):], key)
	copy(buf[2+len(dbNameBytes)+len(key):], value)
	return buf
}
