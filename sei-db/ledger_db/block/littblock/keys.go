package littblock

import (
	"encoding/binary"
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// This file is the single home for the table's key layout.

// Every record kind shares one LittDB table. Each key carries a 1-byte kind prefix
// so the per-kind number spaces never collide:
//
//   - kindBlock     'b' + 8-byte big-endian number (block primary key)
//   - kindBlockHash 'h' + header hash              (block hash alias)
//   - kindQC        'q' + 8-byte big-endian number (QC primary + covered aliases)
//   - kindAppQC     'a' + 8-byte big-endian number (AppQC primary + covered aliases)
//   - kindAppProp   'p' + 8-byte big-endian number (AppProposal primary + covered aliases)
const (
	kindAppQC     byte = 'a'
	kindBlock     byte = 'b'
	kindBlockHash byte = 'h'
	kindAppProp   byte = 'p'
	kindQC        byte = 'q'
)

// kindPrefix returns the key prefix byte for a record kind.
func kindPrefix(kind blocktypes.RecordKind) (byte, error) {
	switch kind {
	case blocktypes.KindBlock:
		return kindBlock, nil
	case blocktypes.KindQC:
		return kindQC, nil
	case blocktypes.KindAppProposal:
		return kindAppProp, nil
	case blocktypes.KindAppQC:
		return kindAppQC, nil
	default:
		return 0, fmt.Errorf("unknown record kind %d", kind)
	}
}

// recordKind returns the record kind a key prefix denotes. The second result is
// false for a prefix that is not number-keyed, such as a hash alias.
func recordKind(prefix byte) (blocktypes.RecordKind, bool) {
	switch prefix {
	case kindBlock:
		return blocktypes.KindBlock, true
	case kindQC:
		return blocktypes.KindQC, true
	case kindAppProp:
		return blocktypes.KindAppProposal, true
	case kindAppQC:
		return blocktypes.KindAppQC, true
	default:
		return 0, false
	}
}

// encodeKey encodes a record number as an 8-byte big-endian value. Big-endian
// is deliberate: it makes lexicographic byte order match numeric order. This is
// the inner codec shared by the prefixed key builders below.
func encodeKey(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

// decodeKey decodes an 8-byte value produced by encodeKey.
func decodeKey(b [8]byte) uint64 {
	return binary.BigEndian.Uint64(b[:])
}

// numberKey returns the key under which a record of the given kind prefix is
// stored at number n — used both for a record's primary key and for each
// covered-number alias.
func numberKey(prefix byte, n uint64) []byte {
	return append([]byte{prefix}, encodeKey(n)...)
}

// blockHashKey returns the secondary (alias) key under which a block is reachable
// by its header hash.
func blockHashKey(hash []byte) []byte {
	return append([]byte{kindBlockHash}, hash...)
}

// keyKind returns the kind prefix byte of a stored key.
func keyKind(key []byte) byte {
	return key[0]
}

// decodeNumberKey decodes the record number from a number-keyed key (i.e. a key
// whose prefix is followed by an 8-byte big-endian number). Panics if key is not
// 9 bytes (1B kind + 8B number).
func decodeNumberKey(key []byte) uint64 {
	return decodeKey([8]byte(key[1:]))
}
