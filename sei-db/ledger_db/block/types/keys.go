package types

import (
	"encoding/binary"
	"fmt"
)

// Key is an encoded primary or secondary lookup key.
type Key []byte

// Key returns the numbered lookup key for kind.
func (kind RecordKind) Key(n BlockNumber) (Key, error) {
	prefix, err := kindPrefix(kind)
	if err != nil {
		return nil, err
	}
	return numberKey(prefix, n), nil
}

// RecordKind returns the primary record kind; lookup-only keys return false.
func (key Key) RecordKind() (RecordKind, bool) {
	return recordKind(keyKind(key))
}

// Number returns the block number in a number-keyed key.
func (key Key) Number() BlockNumber { return decodeNumberKey(key) }

// Reclaimable reports whether the key is eligible below watermark.
func (key Key) Reclaimable(watermark BlockNumber) (bool, error) {
	switch keyKind(key) {
	case kindQCGroup, kindBlock, kindQC, kindAppProp, kindAppQC:
		return key.Number() < watermark, nil
	case kindBlockHash:
		return true, nil
	default:
		return false, fmt.Errorf("unknown ledger key kind %q", key[0])
	}
}

// This file is the single home for the table's key layout.

// Every record kind shares one ledger keyspace. Each key carries a 1-byte kind prefix
// so the per-kind number spaces never collide:
//
//   - kindQCGroup   'g' + 8-byte big-endian first  (packed QC and blocks primary)
//   - kindBlock     'b' + 8-byte big-endian number (secondary block number lookup)
//   - kindBlockHash 'h' + header hash              (block hash alias)
//   - kindQC        'q' + 8-byte big-endian number (secondary QC lookup)
//   - kindAppQC     'a' + 8-byte big-endian number (AppQC primary + covered aliases)
//   - kindAppProp   'p' + 8-byte big-endian number (AppProposal primary + covered aliases)
const (
	kindQCGroup   byte = 'g'
	kindAppQC     byte = 'a'
	kindBlock     byte = 'b'
	kindBlockHash byte = 'h'
	kindAppProp   byte = 'p'
	kindQC        byte = 'q'
)

// kindPrefix returns the key prefix byte for a record kind.
func kindPrefix(kind RecordKind) (byte, error) {
	switch kind {
	case KindQCGroup:
		return kindQCGroup, nil
	case KindBlock:
		return kindBlock, nil
	case KindQC:
		return kindQC, nil
	case KindAppProposal:
		return kindAppProp, nil
	case KindAppQC:
		return kindAppQC, nil
	default:
		return 0, fmt.Errorf("unknown record kind %d", kind)
	}
}

// recordKind returns the primary record kind for a key prefix. Lookup-only
// prefixes do not identify primary records.
func recordKind(prefix byte) (RecordKind, bool) {
	switch prefix {
	case kindQCGroup:
		return KindQCGroup, true
	case kindAppProp:
		return KindAppProposal, true
	case kindAppQC:
		return KindAppQC, true
	default:
		return 0, false
	}
}

// encodeKey encodes a record number as an 8-byte big-endian value. Big-endian
// is deliberate: it makes lexicographic byte order match numeric order. This is
// the inner codec shared by the prefixed key builders below.
func encodeKey(n BlockNumber) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

// decodeKey decodes an 8-byte value produced by encodeKey.
func decodeKey(b [8]byte) BlockNumber {
	return BlockNumber(binary.BigEndian.Uint64(b[:]))
}

// numberKey returns the key under which a record of the given kind prefix is
// stored at number n — used both for a record's primary key and for each
// covered-number alias.
func numberKey(prefix byte, n BlockNumber) Key {
	return append([]byte{prefix}, encodeKey(n)...)
}

// BlockHashKey returns the secondary (alias) key under which a block is reachable
// by its header hash.
func BlockHashKey(hash []byte) Key {
	return append([]byte{kindBlockHash}, hash...)
}

// keyKind returns the kind prefix byte of a stored key.
func keyKind(key []byte) byte {
	return key[0]
}

// decodeNumberKey decodes the record number from a number-keyed key (i.e. a key
// whose prefix is followed by an 8-byte big-endian number). Panics if key is not
// 9 bytes (1B kind + 8B number).
func decodeNumberKey(key []byte) BlockNumber {
	return decodeKey([8]byte(key[1:]))
}
