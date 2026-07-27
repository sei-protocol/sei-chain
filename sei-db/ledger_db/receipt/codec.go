package receipt

import (
	"encoding/binary"
	"fmt"
)

// Receipt values stored in litt begin with a versioned metadata prefix that
// records where the corresponding transaction lives in the block store, so a
// receipt lookup can locate the raw transaction bytes without a separate index:
//
//	[version:1][blockNumber:8 BE][offset:4 BE][length:4 BE][receiptBody...]
//
// blockNumber selects the block; (offset, length) is the sub-range of that
// block's stored value holding the transaction (only meaningful when block
// compression is off — zero otherwise). The leading version byte lets the
// encoding evolve: decodeReceiptData dispatches on it, so a future layout can
// be introduced without breaking readers of older values.
//
// decodeReceiptData also tolerates legacy values written before this prefix
// existed (a raw marshaled Receipt, with no header at all): see the invariant
// documented on receiptDataV1 and on decodeReceiptData.
const (
	// receiptDataV1 is the current receipt value encoding version. Deliberately 1: a legacy,
	// pre-prefix value is a raw marshaled protobuf Receipt, whose leading byte is always a real
	// wire-format tag, (field_number<<3)|wire_type. Protobuf field numbers start at 1, so the
	// smallest possible tag byte is (1<<3)|0 = 8 (field 1, varint) — no valid tag byte can equal 1.
	// That makes the version byte unambiguous: 1 always means "v1 metadata prefix present", and
	// decodeReceiptData treats every other value as a legacy body rather than an error.
	receiptDataV1 byte = 1

	// Field widths of the v1 metadata prefix.
	versionLen     = 1 // version byte
	blockNumberLen = 8 // uint64 block number
	txOffsetLen    = 4 // uint32 tx offset within the block value
	txLengthLen    = 4 // uint32 tx length within the block value

	// receiptDataV1HeaderLen is the fixed prefix width for v1.
	receiptDataV1HeaderLen = versionLen + blockNumberLen + txOffsetLen + txLengthLen
)

// TxHeader is the metadata header describing where a transaction's raw bytes
// live in the block store: the block number, and the (offset, length) sub-range
// of that block's stored value that holds the transaction. Zero-valued when unknown.
type TxHeader struct {
	BlockNumber uint64
	Offset      uint32
	Length      uint32
}

// receiptData is the decoded form of a stored receipt value: the block-store
// location of its transaction plus the marshaled receipt body.
type receiptData struct {
	Header TxHeader
	Body   []byte
}

// encodeReceiptData serializes d using the current (v1) encoding.
func encodeReceiptData(d receiptData) []byte {
	buf := make([]byte, receiptDataV1HeaderLen+len(d.Body))
	buf[0] = receiptDataV1
	binary.BigEndian.PutUint64(buf[versionLen:], d.Header.BlockNumber)
	binary.BigEndian.PutUint32(buf[versionLen+blockNumberLen:], d.Header.Offset)
	binary.BigEndian.PutUint32(buf[versionLen+blockNumberLen+txOffsetLen:], d.Header.Length)
	copy(buf[receiptDataV1HeaderLen:], d.Body)
	return buf
}

// decodeReceiptData parses a stored receipt value, dispatching on the leading
// version byte. Body aliases the input slice (no copy); a caller that retains
// it past the lifetime of the backing buffer must copy it first.
//
// A leading byte other than receiptDataV1 is treated as a legacy, pre-prefix
// value rather than an error: raw is a bare marshaled Receipt, written before
// this metadata prefix existed, so it is returned as-is as Body with a zero
// TxHeader (location unknown). This dispatch is unambiguous, and safe against
// ever misreading a legacy value as v1 or vice versa: see the invariant on
// receiptDataV1 above (no real protobuf tag byte can equal 1).
func decodeReceiptData(raw []byte) (receiptData, error) {
	if len(raw) == 0 {
		return receiptData{}, fmt.Errorf("empty receipt value")
	}
	switch version := raw[0]; version {
	case receiptDataV1:
		if len(raw) < receiptDataV1HeaderLen {
			return receiptData{}, fmt.Errorf("receipt value too short for v%d: %d < %d", receiptDataV1, len(raw), receiptDataV1HeaderLen)
		}
		return receiptData{
			Header: TxHeader{
				BlockNumber: binary.BigEndian.Uint64(raw[versionLen:]),
				Offset:      binary.BigEndian.Uint32(raw[versionLen+blockNumberLen:]),
				Length:      binary.BigEndian.Uint32(raw[versionLen+blockNumberLen+txOffsetLen:]),
			},
			Body: raw[receiptDataV1HeaderLen:],
		}, nil
	default:
		return receiptData{Body: raw}, nil
	}
}
