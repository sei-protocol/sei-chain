package vault

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// The version of the record encoding written by this package. It is independent of the app hash schema version.
const recordVersion uint8 = 1

// Encodes a record as recordVersion followed by the record's canonical app hash serialization.
func serializeRecord(record *apphash.AppHashData) []byte {
	return append([]byte{recordVersion}, record.Serialize()...)
}

// Decodes a record written by serializeRecord(). Errors on an unsupported record version or malformed data.
func deserializeRecord(data []byte) (*apphash.AppHashData, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("hash vault record is empty")
	}
	if data[0] != recordVersion {
		return nil, fmt.Errorf("unsupported hash vault record version %d, want %d", data[0], recordVersion)
	}
	record, err := apphash.Deserialize(data[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize app hash data: %w", err)
	}
	return record, nil
}
