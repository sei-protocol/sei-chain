package vault

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// The version of the record encoding written by this package for a record computed from its inputs. It is
// independent of the app hash schema version.
const recordVersion uint8 = 1

// The version of the record encoding written by this package for a restore anchor: the chain ID, the block height
// and the app hash.
const restoreAnchorRecordVersion uint8 = 2

// The layout of a restore anchor's encoding, which starts with its version byte.
const (
	restoreAnchorChainIDOffset     = 1
	restoreAnchorBlockHeightOffset = restoreAnchorChainIDOffset + 8
	restoreAnchorAppHashOffset     = restoreAnchorBlockHeightOffset + 8
	restoreAnchorRecordSize        = restoreAnchorAppHashOffset + 32
)

// Encodes a record as its record version followed by its fields: the canonical app hash serialization, or for a
// restore anchor, the fields it holds. Errors if the record cannot be serialized.
func serializeRecord(record *apphash.AppHashData) ([]byte, error) {
	if record.IsRestoreAnchor() {
		return serializeRestoreAnchor(record), nil
	}
	data, err := record.Serialize()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize app hash data: %w", err)
	}
	return append([]byte{recordVersion}, data...), nil
}

// Encodes a restore anchor as restoreAnchorRecordVersion followed by its chain ID, block height and app hash.
func serializeRestoreAnchor(anchor *apphash.AppHashData) []byte {
	data := make([]byte, restoreAnchorRecordSize)
	data[0] = restoreAnchorRecordVersion
	binary.BigEndian.PutUint64(data[restoreAnchorChainIDOffset:restoreAnchorBlockHeightOffset], anchor.ChainID())
	binary.BigEndian.PutUint64(data[restoreAnchorBlockHeightOffset:restoreAnchorAppHashOffset], anchor.BlockHeight())
	appHash := anchor.AppHash()
	copy(data[restoreAnchorAppHashOffset:restoreAnchorRecordSize], appHash[:])
	return data
}

// Decodes a record written by serializeRecord(). Errors on an unsupported record version or malformed data.
func deserializeRecord(data []byte) (*apphash.AppHashData, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("hash vault record is empty")
	}
	switch data[0] {
	case recordVersion:
		record, err := apphash.Deserialize(data[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize app hash data: %w", err)
		}
		return record, nil
	case restoreAnchorRecordVersion:
		return deserializeRestoreAnchor(data)
	default:
		return nil, fmt.Errorf("unsupported hash vault record version %d, want %d or %d",
			data[0], recordVersion, restoreAnchorRecordVersion)
	}
}

// Decodes a restore anchor written by serializeRestoreAnchor(). Errors unless data is exactly its length.
func deserializeRestoreAnchor(data []byte) (*apphash.AppHashData, error) {
	if len(data) != restoreAnchorRecordSize {
		return nil, fmt.Errorf("restore anchor record is %d bytes, want %d", len(data), restoreAnchorRecordSize)
	}
	return apphash.NewRestoreAnchorAppHashData(
		binary.BigEndian.Uint64(data[restoreAnchorChainIDOffset:restoreAnchorBlockHeightOffset]),
		binary.BigEndian.Uint64(data[restoreAnchorBlockHeightOffset:restoreAnchorAppHashOffset]),
		[32]byte(data[restoreAnchorAppHashOffset:restoreAnchorRecordSize]),
	), nil
}
