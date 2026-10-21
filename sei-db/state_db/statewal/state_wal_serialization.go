package statewal

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// The version byte that begins a record holding a block's changesets. Records in this format are read but no longer
// written.
const changesetFormatVersion = byte(1)

// The version byte that begins a record holding a block's mutations.
const mutationFormatVersion = byte(2)

// The payload of one block's record in the state WAL.
type stateWALRecord struct {
	// The block's mutations. Unused when isChangeSetFormat is set.
	mutations []mutation.Mutation

	// The block's changesets. Set only when isChangeSetFormat is set.
	changeSets []*proto.NamedChangeSet

	// Whether the record was stored in the changeset format.
	isChangeSetFormat bool
}

// hash returns the block's placeholder hash: the zero hash for a record stored in the changeset format, which earlier
// releases wrote.
func (r *stateWALRecord) hash() [32]byte {
	if r.isChangeSetFormat {
		return [32]byte{}
	}
	return computePlaceholderHash(r.mutations)
}

// toChangeSets returns the block's changesets, converting the record's mutations when it holds mutations.
func (r *stateWALRecord) toChangeSets() ([]*proto.NamedChangeSet, error) {
	if r.isChangeSetFormat {
		return r.changeSets, nil
	}
	changeSets, err := mutation.MutationsToChangeSets(r.mutations)
	if err != nil {
		return nil, fmt.Errorf("failed to convert mutations to changesets: %w", err)
	}
	return changeSets, nil
}

// serializeRecord encodes a record as the mutation format version byte followed by the record's mutations. The block
// number is not encoded: it is the WAL record's index.
func serializeRecord(record stateWALRecord) ([]byte, error) {
	return append([]byte{mutationFormatVersion}, mutation.SerializeMutations(record.mutations)...), nil
}

// deserializeRecord decodes a record in either format, chosen by its leading version byte.
func deserializeRecord(data []byte) (stateWALRecord, error) {
	if len(data) == 0 {
		return stateWALRecord{}, fmt.Errorf("empty record payload: missing version byte")
	}
	switch data[0] {
	case changesetFormatVersion:
		changeSets, err := deserializeChangesets(data)
		if err != nil {
			return stateWALRecord{}, fmt.Errorf("failed to deserialize changesets: %w", err)
		}
		return stateWALRecord{changeSets: changeSets, isChangeSetFormat: true}, nil
	case mutationFormatVersion:
		mutations, err := mutation.DeserializeMutations(data[1:])
		if err != nil {
			return stateWALRecord{}, fmt.Errorf("failed to deserialize mutations: %w", err)
		}
		return stateWALRecord{mutations: mutations}, nil
	default:
		return stateWALRecord{}, fmt.Errorf("unsupported record format version %d", data[0])
	}
}

// deserializeChangesets decodes a changeset format payload, [version]([uvarint length][marshaled NamedChangeSet])*,
// after checking its leading version byte. Because the enclosing WAL record is length-delimited and CRC-verified by
// the underlying WAL, any truncation encountered here indicates corruption and is reported as an error rather than
// tolerated.
func deserializeChangesets(data []byte) ([]*proto.NamedChangeSet, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty changeset payload: missing version byte")
	}
	if version := data[0]; version != changesetFormatVersion {
		return nil, fmt.Errorf("unsupported changeset format version %d", version)
	}

	var result []*proto.NamedChangeSet
	rest := data[1:]
	for len(rest) > 0 {
		length, n := binary.Uvarint(rest)
		if n <= 0 {
			return nil, fmt.Errorf("corrupt changeset length prefix")
		}
		rest = rest[n:]
		if uint64(len(rest)) < length {
			return nil, fmt.Errorf("changeset payload truncated: need %d bytes, have %d", length, len(rest))
		}
		ncs := &proto.NamedChangeSet{}
		if err := ncs.Unmarshal(rest[:length]); err != nil {
			return nil, fmt.Errorf("failed to unmarshal changeset: %w", err)
		}
		rest = rest[length:]
		result = append(result, ncs)
	}
	return result, nil
}
