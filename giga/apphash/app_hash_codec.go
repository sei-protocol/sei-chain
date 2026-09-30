package apphash

import (
	"encoding/binary"
	"fmt"
)

// Design note: protobuf format intentionally not used, as protobuf serialization is notoriously non-deterministic.

// Byte offsets of each field in the canonical format, which is the fields in declaration order with no padding.
const (
	versionOffset         = 0
	blockHeightOffset     = versionOffset + 1
	blockHeaderHashOffset = blockHeightOffset + 8
	stateHashOffset       = blockHeaderHashOffset + 32
	budOffset             = stateHashOffset + 32
	receiptHashOffset     = budOffset + 32
	previousAppHashOffset = receiptHashOffset + 32

	// serializedSize is the length of the canonical format.
	serializedSize = previousAppHashOffset + 32
)

// Serialize the app hash into the canonical byte format.
// Produces deterministic output.
func (ahd *AppHashData) Serialize() []byte {
	data := make([]byte, 0, serializedSize)
	data = append(data, ahd.version)
	data = binary.BigEndian.AppendUint64(data, ahd.blockHeight)
	data = append(data, ahd.blockHeaderHash[:]...)
	data = append(data, ahd.stateHash[:]...)
	data = append(data, ahd.bud[:]...)
	data = append(data, ahd.receiptHash[:]...)
	data = append(data, ahd.previousAppHash[:]...)
	return data
}

// Deserialize the app hash from the canonical byte format. It returns an error unless data starts with a
// supported schema version and is exactly the length that version defines.
func Deserialize(data []byte) (*AppHashData, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("app hash data is empty")
	}
	if version := data[versionOffset]; version != appHashVersion {
		return nil, fmt.Errorf("unsupported app hash version %d, want %d", version, appHashVersion)
	}
	if len(data) != serializedSize {
		return nil, fmt.Errorf("app hash data is %d bytes, want %d", len(data), serializedSize)
	}
	return NewAppHashData(
		binary.BigEndian.Uint64(data[blockHeightOffset:blockHeaderHashOffset]),
		[32]byte(data[blockHeaderHashOffset:stateHashOffset]),
		[32]byte(data[stateHashOffset:budOffset]),
		[32]byte(data[budOffset:receiptHashOffset]),
		[32]byte(data[receiptHashOffset:previousAppHashOffset]),
		[32]byte(data[previousAppHashOffset:serializedSize]),
	), nil
}
