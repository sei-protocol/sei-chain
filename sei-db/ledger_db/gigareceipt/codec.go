package gigareceipt

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
// blockNumber selects the block; (offset, length) is the transaction's sub-range
// within that block's marshalled proto(Block) body, excluding the block store's
// value framing. A length of 0 means the location is unknown. The leading version
// byte lets the encoding evolve: decodeReceiptData dispatches on it, so a future
// layout can be introduced without breaking readers of older values.
//
// decodeReceiptData also tolerates legacy values written before this prefix
// existed (a raw marshaled Receipt, with no header at all): see
// minProtobufTagByte and decodeReceiptData.
const (
	// receiptDataV1 is the current receipt value encoding version.
	receiptDataV1 byte = 1

	// minProtobufTagByte is the smallest leading byte of a marshaled protobuf message: the tag
	// (field_number<<3)|wire_type for field 1, wire type 0. No legacy receipt starts below it, so
	// leading bytes 0x00-0x07 are reserved for encoding versions.
	minProtobufTagByte byte = 1 << 3

	// Field widths of the v1 metadata prefix.
	versionLen     = 1 // version byte
	blockNumberLen = 8 // uint64 block number
	txOffsetLen    = 4 // uint32 tx offset within the block body
	txLengthLen    = 4 // uint32 tx length within the block body

	// receiptDataV1HeaderLen is the fixed prefix width for v1.
	receiptDataV1HeaderLen = versionLen + blockNumberLen + txOffsetLen + txLengthLen
)

// TxHeader locates a transaction's raw bytes in the block store: its block number,
// and its (offset, length) within that block's marshalled proto(Block) body,
// excluding the block store's value framing. Length is 0 when the location is unknown.
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

// decodeReceiptData parses a stored receipt value, dispatching on its leading byte. Body
// aliases the input slice; a caller that retains it past the backing buffer's lifetime must
// copy it first.
//
// A leading byte of minProtobufTagByte or above marks a legacy value, a bare marshaled
// Receipt with no metadata prefix, returned whole as Body with a zero TxHeader. A reserved
// leading byte that is not a known version is rejected.
func decodeReceiptData(raw []byte) (receiptData, error) {
	if len(raw) == 0 {
		return receiptData{}, fmt.Errorf("empty receipt value")
	}
	switch version := raw[0]; {
	case version == receiptDataV1:
		if len(raw) < receiptDataV1HeaderLen {
			return receiptData{}, fmt.Errorf("receipt value too short for v%d: %d < %d",
				receiptDataV1, len(raw), receiptDataV1HeaderLen)
		}
		return receiptData{
			Header: TxHeader{
				BlockNumber: binary.BigEndian.Uint64(raw[versionLen:]),
				Offset:      binary.BigEndian.Uint32(raw[versionLen+blockNumberLen:]),
				Length:      binary.BigEndian.Uint32(raw[versionLen+blockNumberLen+txOffsetLen:]),
			},
			Body: raw[receiptDataV1HeaderLen:],
		}, nil
	case version < minProtobufTagByte:
		return receiptData{}, fmt.Errorf("unsupported receipt encoding version %d", version)
	default:
		return receiptData{Body: raw}, nil
	}
}
