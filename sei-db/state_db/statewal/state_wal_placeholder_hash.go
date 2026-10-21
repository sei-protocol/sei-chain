package statewal

import (
	"bytes"
	"crypto/sha256"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// computePlaceholderHash returns the hash of a block's mutations: SHA-256 over each mutation's serialization, in key
// order. mutations is not modified.
//
// STOPGAP: this is a placeholder, intentionally not a BUD as the BUD spec defines it.
func computePlaceholderHash(mutations []mutation.Mutation) [32]byte {
	// This placeholder exists so the API that delivers hashes can be built and run on a private testnet before the
	// real BUD lands. It is replaced wholesale then. Its known shortcuts, all deliberate:
	//
	//   - Every block is serialized twice, once for its WAL record and once here.
	//   - The iterator recomputes a block's hash by serializing it again. The real BUD starts from the BUD tree, or
	//     hashes will be stored in the WAL and read back rather than recomputed.
	//   - A block stored in the changeset format of earlier releases hashes to the zero hash.
	order := make([]int, len(mutations))
	for i := range order {
		order[i] = i
	}
	// The order of a block's mutations carries no meaning, so it must not change the hash.
	slices.SortFunc(order, func(a int, b int) int {
		return bytes.Compare(mutations[a].Key, mutations[b].Key)
	})

	h := sha256.New()
	for _, i := range order {
		_, _ = h.Write(mutations[i].Serialize()) // hash.Hash.Write never returns an error
	}
	var hash [32]byte
	h.Sum(hash[:0])
	return hash
}
