package statewal

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// computePlaceholderHash returns the hash of a block's changesets: SHA-256 over each changeset's protobuf encoding,
// each prefixed by its length as a uvarint.
//
// STOPGAP: this is a placeholder, intentionally not a BUD as the BUD spec defines it.
func computePlaceholderHash(cs []*proto.NamedChangeSet) ([32]byte, error) {
	// This placeholder exists so the API that delivers hashes can be built and run on a private testnet before the
	// real BUD lands. It is replaced wholesale then. Its known shortcuts, all deliberate:
	//
	//   - Protobuf encoding is not deterministic by specification. It is deterministic in practice for a fixed
	//     protobuf library version and compiler, which a private testnet can pin across every node. A public chain
	//     cannot pin every validator's build, and nodes on different builds could disagree on a block's hash, so
	//     this must not ship there.
	//   - Every block is marshaled twice, once for its WAL record and once here.
	//   - The iterator recomputes a block's hash by marshaling it again. The real BUD starts from the BUD tree, or
	//     hashes will be stored in the WAL and read back rather than recomputed.
	//   - The input is its own encoding rather than the WAL record. The hash is a function of the changesets, not
	//     of how they are stored, so a change to the record format must not change any block's hash.
	h := sha256.New()
	var lengthPrefix [binary.MaxVarintLen64]byte
	for i, ncs := range cs {
		if ncs == nil {
			return [32]byte{}, fmt.Errorf("changeset at index %d is nil", i)
		}
		marshaled, err := ncs.Marshal()
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to marshal changeset at index %d: %w", i, err)
		}
		n := binary.PutUvarint(lengthPrefix[:], uint64(len(marshaled)))
		_, _ = h.Write(lengthPrefix[:n]) // hash.Hash.Write never returns an error
		_, _ = h.Write(marshaled)
	}
	var hash [32]byte
	h.Sum(hash[:0])
	return hash, nil
}
