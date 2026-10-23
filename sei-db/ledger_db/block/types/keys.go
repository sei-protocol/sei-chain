package types

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Key is an encoded primary or secondary lookup key.
type Key []byte

// Key returns the numbered lookup key for kind, panicking for an unknown kind.
func (kind RecordKind) Key(n BlockNumber) Key {
	return numberKey(kindPrefix(kind), n)
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
		return false, fmt.Errorf("unknown ledger key kind in %x", key)
	}
}

// This file is the single home for the table's key layout.

// Every key starts with an unsigned varint kind, followed by either an
// 8-byte big-endian block number or a block header hash.
const (
	kindQCGroup uint64 = iota + 1
	kindAppQC
	kindBlock
	kindBlockHash
	kindAppProp
	kindQC
)

// kindPrefix returns the key prefix for a record kind.
func kindPrefix(kind RecordKind) uint64 {
	switch kind {
	case KindQCGroup:
		return kindQCGroup
	case KindBlock:
		return kindBlock
	case KindQC:
		return kindQC
	case KindAppProposal:
		return kindAppProp
	case KindAppQC:
		return kindAppQC
	default:
		panic(fmt.Errorf("unknown record kind %d", kind))
	}
}

// recordKind returns the primary record kind for a key prefix. Lookup-only
// prefixes do not identify primary records.
func recordKind(prefix uint64) (RecordKind, bool) {
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
func numberKey(prefix uint64, n BlockNumber) Key {
	return append(binary.AppendUvarint(nil, prefix), encodeKey(n)...)
}

// BlockHashKey returns the secondary (alias) key under which a block is reachable
// by its header hash.
func BlockHashKey(hash BlockHash) Key {
	return append(binary.AppendUvarint(nil, kindBlockHash), hash...)
}

// keyKind returns the unsigned varint kind of a stored key.
func keyKind(key []byte) uint64 {
	kind, n := binary.Uvarint(key)
	if n <= 0 {
		return math.MaxUint64
	}
	return kind
}

// decodeNumberKey decodes a key's block number after its varint kind.
func decodeNumberKey(key []byte) BlockNumber {
	_, n := binary.Uvarint(key)
	return decodeKey([8]byte(key[n:]))
}
