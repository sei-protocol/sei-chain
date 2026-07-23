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
const (
	// receiptDataV1 is the current receipt value encoding version.
	receiptDataV1 byte = 1

	// Field widths of the v1 metadata prefix.
	versionLen     = 1 // version byte
	blockNumberLen = 8 // uint64 block number
	txOffsetLen    = 4 // uint32 tx offset within the block value
	txLengthLen    = 4 // uint32 tx length within the block value

	// receiptDataV1HeaderLen is the fixed prefix width for v1.
	receiptDataV1HeaderLen = versionLen + blockNumberLen + txOffsetLen + txLengthLen
)

// receiptData is the decoded form of a stored receipt value: the block-store
// location of its transaction plus the marshaled receipt body.
type receiptData struct {
	BlockNumber uint64
	TxOffset    uint32
	TxLength    uint32
	Body        []byte
}

// encodeReceiptData serializes d using the current (v1) encoding.
func encodeReceiptData(d receiptData) []byte {
	buf := make([]byte, receiptDataV1HeaderLen+len(d.Body))
	buf[0] = receiptDataV1
	binary.BigEndian.PutUint64(buf[versionLen:], d.BlockNumber)
	binary.BigEndian.PutUint32(buf[versionLen+blockNumberLen:], d.TxOffset)
	binary.BigEndian.PutUint32(buf[versionLen+blockNumberLen+txOffsetLen:], d.TxLength)
	copy(buf[receiptDataV1HeaderLen:], d.Body)
	return buf
}

// decodeReceiptData parses a stored receipt value, dispatching on the leading
// version byte. Body aliases the input slice (no copy); a caller that retains
// it past the lifetime of the backing buffer must copy it first.
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
			BlockNumber: binary.BigEndian.Uint64(raw[versionLen:]),
			TxOffset:    binary.BigEndian.Uint32(raw[versionLen+blockNumberLen:]),
			TxLength:    binary.BigEndian.Uint32(raw[versionLen+blockNumberLen+txOffsetLen:]),
			Body:        raw[receiptDataV1HeaderLen:],
		}, nil
	default:
		return receiptData{}, fmt.Errorf("unknown receipt value version %d", version)
	}
}
