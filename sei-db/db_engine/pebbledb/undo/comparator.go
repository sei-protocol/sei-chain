package undo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/cockroachdb/pebble/v2"
)

// Every key in an undo database has the shape
//
//	<body> 0x00 [<height> <0x09>]
//
// where <body> is the part compared byte-wise, 0x00 is a sentinel ending it, and the optional tail
// is an 8-byte big-endian block height followed by a byte counting the sentinel and the height. A
// key without the tail is a bare prefix: bucket boundaries and metadata keys take that form. This
// is the layout the MVCC state store uses, with the height ascending rather than complemented, so
// the first record at or above a height is a forward seek.
//
// A record's body is
//
//	<bucket(8, big-endian)> <key>
//
// so all records of one bucket form a single contiguous key range, and Pebble's prefix (Split) is
// everything but the height: a prefix Bloom filter answers "does this bucket hold this key" without
// reading a data block. Metadata keys sit in metadataBucket.
const (
	bucketLen = 8
	heightLen = 8

	// suffixLen is the value of a record key's last byte: the sentinel and the height it follows.
	suffixLen = 1 + heightLen

	// metadataBucket holds the metadata keys. Heights are int64, so no record reaches it.
	metadataBucket = math.MaxUint64
)

// Comparer orders undo keys by body, then by height. It is allocation-free: Pebble calls Split and
// Compare for every key it flushes, compacts or seeks.
var Comparer = &pebble.Comparer{
	Name: "ss_undolog_comparator",

	Compare: compareKeys,

	// A key has one encoding, so byte equality is key equality.
	Equal: bytes.Equal,

	AbbreviatedKey: func(k []byte) uint64 {
		return pebble.DefaultComparer.AbbreviatedKey(keyBody(k))
	},

	Separator: func(dst, a, b []byte) []byte {
		aBody, bBody := keyBody(a), keyBody(b)
		if bytes.Equal(aBody, bBody) {
			return append(dst, a...)
		}
		n := len(dst)
		dst = pebble.DefaultComparer.Separator(dst, aBody, bBody)
		if bytes.Equal(dst[n:], aBody) {
			return append(dst[:n], a...)
		}
		// The separator sorts above a's body, so as a bare prefix it sorts above a itself.
		return append(dst, 0)
	},

	Successor: func(dst, a []byte) []byte {
		aBody := keyBody(a)
		n := len(dst)
		dst = pebble.DefaultComparer.Successor(dst, aBody)
		if bytes.Equal(dst[n:], aBody) {
			return append(dst[:n], a...)
		}
		return append(dst, 0)
	},

	ImmediateSuccessor: func(dst, a []byte) []byte {
		// a is a bare prefix "<body>\x00"; the next bare prefix has the body "<body>\x00".
		return append(append(dst, a...), 0)
	},

	Split: splitKey,

	ComparePointSuffixes: bytes.Compare,
	CompareRangeSuffixes: bytes.Compare,

	FormatKey: func(k []byte) fmt.Formatter {
		return keyFormatter(k)
	},
}

// splitKey returns the length of k's prefix: the body and its sentinel.
func splitKey(k []byte) int {
	last := len(k) - 1
	if last < 0 {
		return 0
	}
	tail := int(k[last])
	if tail > last {
		return len(k)
	}
	return last - tail + 1
}

// keyBody returns the byte-wise compared part of k, without the sentinel and the height.
func keyBody(k []byte) []byte {
	last := len(k) - 1
	if last < 0 {
		return k
	}
	sep := last - int(k[last])
	if sep < 0 {
		return k
	}
	return k[:sep]
}

// compareKeys orders keys by body, then by height, with a bare prefix before every key sharing
// its body.
func compareKeys(a, b []byte) int {
	aLast, bLast := len(a)-1, len(b)-1
	if aLast < 0 || bLast < 0 {
		return bytes.Compare(a, b)
	}
	aSep, bSep := aLast-int(a[aLast]), bLast-int(b[bLast])
	if aSep < 0 || bSep < 0 {
		return bytes.Compare(a, b)
	}
	if c := bytes.Compare(a[:aSep], b[:bSep]); c != 0 {
		return c
	}
	aTail, bTail := a[aSep:aLast], b[bSep:bLast]
	switch {
	case len(aTail) == 0 && len(bTail) == 0:
		return 0
	case len(aTail) == 0:
		return -1
	case len(bTail) == 0:
		return 1
	}
	return bytes.Compare(aTail, bTail)
}

// recordKeyLen returns the length of the record key encodeRecordKey writes for key.
func recordKeyLen(key []byte) int {
	return bucketLen + len(key) + 1 + heightLen + 1
}

// encodeRecordKey writes the record key for key's value before block height, which falls in
// bucket, into dst. dst must be exactly recordKeyLen bytes.
func encodeRecordKey(dst []byte, bucket uint64, key []byte, height uint64) {
	binary.BigEndian.PutUint64(dst, bucket)
	n := bucketLen + copy(dst[bucketLen:], key)
	dst[n] = 0
	binary.BigEndian.PutUint64(dst[n+1:], height)
	dst[n+1+heightLen] = suffixLen
}

// appendRecordKey appends the record key encodeRecordKey writes to dst.
func appendRecordKey(dst []byte, bucket uint64, key []byte, height uint64) []byte {
	n, size := len(dst), recordKeyLen(key)
	dst = slices.Grow(dst, size)[:n+size]
	encodeRecordKey(dst[n:], bucket, key, height)
	return dst
}

// decodeRecordHeight returns the block height of a record key.
func decodeRecordHeight(k []byte) (uint64, error) {
	if len(k) < suffixLen+1 || k[len(k)-1] != suffixLen {
		return 0, fmt.Errorf("undo: key %x is not an undo record", k)
	}
	return binary.BigEndian.Uint64(k[len(k)-1-heightLen:]), nil
}

// bucketBoundary returns the bare prefix that sorts before every record of bucket and after every
// record of the buckets below it. Excising [bucketBoundary(a), bucketBoundary(b)) removes buckets
// a through b-1 and nothing else.
func bucketBoundary(bucket uint64) []byte {
	k := make([]byte, bucketLen+1)
	binary.BigEndian.PutUint64(k, bucket)
	return k
}

// metadataKey returns the bare prefix a metadata value is stored under.
func metadataKey(name string) []byte {
	return append(append(bucketBoundary(metadataBucket)[:bucketLen], name...), 0)
}

// keyFormatter renders a key as its body and, for a record, the height it was written at.
type keyFormatter []byte

func (k keyFormatter) Format(s fmt.State, _ rune) {
	body := keyBody(k)
	if height, err := decodeRecordHeight(k); err == nil {
		_, _ = fmt.Fprintf(s, "%x@%d", body, height)
		return
	}
	_, _ = fmt.Fprintf(s, "%x", body)
}
