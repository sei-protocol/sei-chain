package statewal

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// The version byte that begins a serialized block holding changesets. Blocks in this format are read but no longer
// written.
const changesetFormatVersion = byte(1)

// The version byte that begins a serialized block holding mutations.
const mutationFormatVersion = byte(2)

// serializeBlock returns the serialization of a block's mutations: the mutation format version byte, then the
// mutations' serialization. The block number is not encoded: it is the block's index in the WAL.
func serializeBlock(mutations []mutation.Mutation) []byte {
	return append([]byte{mutationFormatVersion}, mutation.SerializeMutations(mutations)...)
}

// deserializeBlock decodes a serialized block in either format, chosen by its leading version byte, into the block's
// changesets.
func deserializeBlock(serialized []byte) ([]*proto.NamedChangeSet, error) {
	if len(serialized) == 0 {
		return nil, fmt.Errorf("empty serialized block: missing version byte")
	}
	switch serialized[0] {
	case changesetFormatVersion:
		changeSets, err := deserializeLegacyBlock(serialized)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize changesets: %w", err)
		}
		return changeSets, nil
	case mutationFormatVersion:
		mutations, err := mutation.DeserializeMutations(serialized[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize mutations: %w", err)
		}
		changeSets, err := mutation.MutationsToChangeSets(mutations)
		if err != nil {
			return nil, fmt.Errorf("failed to convert mutations to changesets: %w", err)
		}
		return changeSets, nil
	default:
		return nil, fmt.Errorf("unsupported block format version %d", serialized[0])
	}
}

// deserializeLegacyBlock decodes a block serialized in the legacy changeset format,
// [version]([uvarint length][marshaled NamedChangeSet])*,
// after checking its leading version byte. Because the enclosing WAL record is length-delimited and CRC-verified by
// the underlying WAL, any truncation encountered here indicates corruption and is reported as an error rather than
// tolerated.
func deserializeLegacyBlock(data []byte) ([]*proto.NamedChangeSet, error) {
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
