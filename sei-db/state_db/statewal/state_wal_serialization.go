package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// The version byte that begins a serialized block holding mutations.
const mutationFormatVersion = byte(2)

// serializeBlock returns the serialization of a block's mutations: the mutation format version byte, then the
// mutations' serialization. The block number is not encoded: it is the block's index in the WAL.
func serializeBlock(mutations []mutation.Mutation) []byte {
	return append([]byte{mutationFormatVersion}, mutation.SerializeMutations(mutations)...)
}

// deserializeBlock decodes a serialized block into the block's mutations. It returns an error if the block does not
// begin with mutationFormatVersion.
func deserializeBlock(serialized []byte) ([]mutation.Mutation, error) {
	if err := requireMutationFormat(serialized); err != nil {
		return nil, err
	}
	mutations, err := mutation.DeserializeMutations(serialized[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize mutations: %w", err)
	}
	return mutations, nil
}

// requireMutationFormat returns an error unless serialized begins with mutationFormatVersion.
func requireMutationFormat(serialized []byte) error {
	if len(serialized) == 0 {
		return fmt.Errorf("empty serialized block: missing version byte")
	}
	if serialized[0] != mutationFormatVersion {
		return fmt.Errorf("unsupported block format version %d", serialized[0])
	}
	return nil
}
