package pebbleblockdb

import "encoding/binary"

// Key prefixes. Each prefix occupies a distinct byte range so Pebble
// keeps the namespaces physically separated in the LSM.
const (
	prefixBlock   byte = 'B' // B + uint64BE(height)                  → block header
	prefixTxData  byte = 'X' // X + uint64BE(height) + uint32BE(idx)  → raw tx bytes
	prefixHashIdx byte = 'H' // H + blockHash                         → uint64BE(height)
	prefixTxIdx   byte = 'T' // T + txHash                            → uint64BE(height) + uint32BE(idx)
	prefixMeta    byte = 'M' // M + tag                               → uint64BE(value)
	metaTagLo     byte = 'l'
	metaTagHi     byte = 'h'
)

func encodeBlockKey(height uint64) []byte {
	k := make([]byte, 9)
	k[0] = prefixBlock
	binary.BigEndian.PutUint64(k[1:], height)
	return k
}

func encodeTxDataKey(height uint64, txIndex uint32) []byte {
	k := make([]byte, 13)
	k[0] = prefixTxData
	binary.BigEndian.PutUint64(k[1:], height)
	binary.BigEndian.PutUint32(k[9:], txIndex)
	return k
}

func encodeHashIdxKey(hash []byte) []byte {
	k := make([]byte, 1+len(hash))
	k[0] = prefixHashIdx
	copy(k[1:], hash)
	return k
}

func encodeTxIdxKey(txHash []byte) []byte {
	k := make([]byte, 1+len(txHash))
	k[0] = prefixTxIdx
	copy(k[1:], txHash)
	return k
}

func encodeTxIdxValue(height uint64, txIndex uint32) []byte {
	v := make([]byte, 12)
	binary.BigEndian.PutUint64(v[0:], height)
	binary.BigEndian.PutUint32(v[8:], txIndex)
	return v
}

func decodeTxIdxValue(v []byte) (height uint64, txIndex uint32) {
	height = binary.BigEndian.Uint64(v[0:8])
	txIndex = binary.BigEndian.Uint32(v[8:12])
	return
}

func encodeHeightValue(height uint64) []byte {
	v := make([]byte, 8)
	binary.BigEndian.PutUint64(v, height)
	return v
}

func decodeHeightValue(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

func metaKeyLo() []byte { return []byte{prefixMeta, metaTagLo} }
func metaKeyHi() []byte { return []byte{prefixMeta, metaTagHi} }

// Range helpers for DeleteRange (upper bound is exclusive).

func blockKeyRangeForPrune(loHeight, hiHeight uint64) (start, end []byte) {
	return encodeBlockKey(loHeight), encodeBlockKey(hiHeight)
}

func txDataKeyRangeForPrune(loHeight, hiHeight uint64) (start, end []byte) {
	start = make([]byte, 13)
	start[0] = prefixTxData
	binary.BigEndian.PutUint64(start[1:], loHeight)

	end = make([]byte, 13)
	end[0] = prefixTxData
	binary.BigEndian.PutUint64(end[1:], hiHeight)
	return
}
